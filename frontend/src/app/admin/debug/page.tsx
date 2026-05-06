"use client";

import { useMemo, useState } from "react";
import useSWR from "swr";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { copyToClipboard } from "@/lib/clipboard";

const fetcher = async (url: string) => {
  const res = await fetch(url, { cache: "no-store" });
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error(data?.error || "request_failed");
  }
  return data;
};

type TokenHeader = "authorization" | "x-api-key" | "x-goog-api-key";
type TestMode = "models" | "chat" | "responses" | "embeddings" | "claude" | "gemini_generate" | "gemini_stream";

type MasterKeyItem = {
  id: number;
  name: string;
  maskedKey: string;
  status: string;
};

type MasterKeyResponse = {
  keys: MasterKeyItem[];
};

type SystemConfig = {
  gatewayBaseURL: string;
};

type DebugResult = {
  ok: boolean;
  status: number;
  statusText: string;
  contentType: string;
  durationMs: number;
  headers: Record<string, string>;
  bodyText: string;
  bodyJson: unknown;
  error?: string;
};

type UpstreamDebugInfo = {
  operation: string;
  keyName: string;
  keyChain: string;
  attempts: string;
  switched: string;
  lastError: string;
};

type BatchItem = {
  mode: TestMode;
  label: string;
  ok: boolean;
  status: number;
  statusText: string;
  durationMs: number;
  contentType: string;
  headers: Record<string, string>;
  bodyText: string;
  bodyJson: unknown;
  error?: string;
  upstream: UpstreamDebugInfo | null;
};

const modeLabels: Record<TestMode, string> = {
  models: "获取模型列表",
  chat: "OpenAI Chat",
  responses: "OpenAI Responses",
  embeddings: "OpenAI Embeddings",
  claude: "Claude Messages",
  gemini_generate: "Gemini Generate",
  gemini_stream: "Gemini Stream",
};

function parseUpstreamDebugInfo(result: DebugResult | null): UpstreamDebugInfo | null {
  if (!result) return null;
  return {
    operation: result.headers["x-gateway-upstream-operation"] || "",
    keyName: result.headers["x-gateway-upstream-key-name"] || "",
    keyChain: result.headers["x-gateway-upstream-key-chain"] || "",
    attempts: result.headers["x-gateway-upstream-attempts"] || "0",
    switched: result.headers["x-gateway-upstream-switched"] || "false",
    lastError: result.headers["x-gateway-upstream-last-error"] || "",
  };
}

export default function DebugGatewayPage() {
  const { data: masterKeysData, error: masterKeysError } = useSWR<MasterKeyResponse>("/api/master-keys", fetcher);
  const { data: systemConfig } = useSWR<SystemConfig>("/api/system/config", fetcher);

  const masterKeys = masterKeysData?.keys ?? [];
  const gatewayBaseURL = systemConfig?.gatewayBaseURL || "http://127.0.0.1:18080";

  const [testMode, setTestMode] = useState<TestMode>("models");
  const [method, setMethod] = useState<"GET" | "POST">("GET");
  const [path, setPath] = useState("/v1/models");
  const [tokenHeader, setTokenHeader] = useState<TokenHeader>("authorization");
  const [token, setToken] = useState("");
  const [extraHeadersText, setExtraHeadersText] = useState("");
  const [body, setBody] = useState("");
  const [selectedMasterKeyId, setSelectedMasterKeyId] = useState("");
  const [loadingToken, setLoadingToken] = useState(false);
  const [sending, setSending] = useState(false);
  const [loadingModels, setLoadingModels] = useState(false);
  const [models, setModels] = useState<string[]>([]);
  const [selectedModel, setSelectedModel] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<DebugResult | null>(null);
  const [batchResults, setBatchResults] = useState<BatchItem[]>([]);

  const copyCurlPreview = async () => {
    const copied = await copyToClipboard(curlPreview);
    if (copied) {
      setMessage("已复制 curl 命令");
      setError(null);
      return;
    }
    setError("复制 curl 失败，请手动复制。");
  };

  function applyRecommendedTemplate(mode: TestMode, model: string) {
    const payload = buildTemplate(mode, model);
    setMethod(payload.method);
    setPath(payload.path);
    setTokenHeader(payload.tokenHeader);
    setExtraHeadersText(payload.extraHeadersText);
    setBody(payload.body);
  }

  const loadMasterKey = async () => {
    if (!selectedMasterKeyId) {
      setError("请先选择一个自定义 API Key。");
      return;
    }
    setLoadingToken(true);
    setError(null);
    setMessage(null);
    try {
      const res = await fetch(`/api/master-keys/${selectedMasterKeyId}/reveal`, { method: "POST" });
      const payload = await res.json().catch(() => null);
      if (!res.ok || !payload?.plainKey) {
        setError(payload?.error || "载入 API Key 失败。");
        return;
      }
      setToken(payload.plainKey);
      setMessage(`已载入 ${payload?.name || "自定义 API Key"}`);
    } catch {
      setError("载入 API Key 失败。");
    } finally {
      setLoadingToken(false);
    }
  };

  const sendDebugRequest = async (override?: Partial<{ mode: TestMode; path: string; method: "GET" | "POST"; tokenHeader: TokenHeader; body: string; extraHeadersText: string; }>) => {
    const finalPath = override?.path ?? path;
    const finalMethod = override?.method ?? method;
    const finalTokenHeader = override?.tokenHeader ?? tokenHeader;
    const finalBody = override?.body ?? body;
    const finalHeadersText = override?.extraHeadersText ?? extraHeadersText;

    const headers: Record<string, string> = {};
    if (finalHeadersText.trim()) {
      try {
        Object.assign(headers, JSON.parse(finalHeadersText));
      } catch {
        throw new Error("额外请求头 JSON 无法解析");
      }
    }

    const res = await fetch("/api/debug/gateway", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path: finalPath, method: finalMethod, token, tokenHeader: finalTokenHeader, headers, body: finalMethod === "GET" ? "" : finalBody }),
    });
    return (await res.json()) as DebugResult;
  };

  const sendRequest = async () => {
    setSending(true);
    setError(null);
    setMessage(null);
    try {
      const payload = await sendDebugRequest();
      setResult(payload);
      if (!payload.ok) {
        setError(payload.error || `请求返回 ${payload.status || 500}`);
      } else {
        setMessage(`请求完成：${payload.status} ${payload.statusText}`);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "调试请求失败。");
    } finally {
      setSending(false);
    }
  };

  const loadModels = async () => {
    setLoadingModels(true);
    setError(null);
    setMessage(null);
    try {
      const payload = await sendDebugRequest({ mode: "models", path: "/v1/models", method: "GET", tokenHeader: "authorization", body: "", extraHeadersText: "" });
      setResult(payload);
      if (!payload.ok) {
        setError(payload.error || "获取模型列表失败。");
        return;
      }
      const data = (payload.bodyJson as { data?: Array<{ id?: string }> } | null)?.data ?? [];
      const modelIds = Array.from(new Set(data.map((item) => item.id || "").filter(Boolean)));
      setModels(modelIds);
      if (!selectedModel && modelIds[0]) {
        setSelectedModel(modelIds[0]);
        applyRecommendedTemplate(testMode, modelIds[0]);
      }
      setMessage(`已获取 ${modelIds.length} 个模型`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "获取模型列表失败。");
    } finally {
      setLoadingModels(false);
    }
  };

  const runBatchTest = async () => {
    if (!selectedModel) {
      setError("请先获取模型列表并选择一个模型。");
      return;
    }
    setSending(true);
    setError(null);
    setMessage(null);
    setBatchResults([]);
    const plan: TestMode[] = isEmbeddingCandidate(selectedModel)
      ? ["embeddings"]
      : ["chat", "responses", "claude", "gemini_generate"];
    const items: BatchItem[] = [];
    let lastPayload: DebugResult | null = null;
    try {
      for (const mode of plan) {
        const preset = buildTemplate(mode, selectedModel);
        const payload = await sendDebugRequest({
          mode,
          path: preset.path,
          method: preset.method,
          tokenHeader: preset.tokenHeader,
          body: preset.body,
          extraHeadersText: preset.extraHeadersText,
        });
        lastPayload = payload;
        items.push({
          mode,
          label: modeLabels[mode],
          ok: payload.ok,
          status: payload.status,
          statusText: payload.statusText,
          durationMs: payload.durationMs,
          contentType: payload.contentType,
          headers: payload.headers,
          bodyText: payload.bodyText,
          bodyJson: payload.bodyJson,
          error: payload.ok ? undefined : payload.error || payload.statusText,
          upstream: parseUpstreamDebugInfo(payload),
        });
      }
      setBatchResults(items);
      if (lastPayload) {
        setResult(lastPayload);
      }
      setMessage(`已完成 ${items.length} 项当前模型测试，并返回每项请求结果`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "批量测试失败。");
    } finally {
      setSending(false);
    }
  };

  const curlPreview = (() => {
    const headerLines: string[] = [];
    if (token.trim()) {
      if (tokenHeader === "authorization") headerLines.push(`-H "Authorization: Bearer ${token.trim()}"`);
      else if (tokenHeader === "x-api-key") headerLines.push(`-H "x-api-key: ${token.trim()}"`);
      else headerLines.push(`-H "x-goog-api-key: ${token.trim()}"`);
    }
    if (extraHeadersText.trim()) {
      try {
        const parsed = JSON.parse(extraHeadersText) as Record<string, string>;
        for (const [key, value] of Object.entries(parsed)) {
          if (key.trim() && String(value).trim()) headerLines.push(`-H "${key}: ${String(value)}"`);
        }
      } catch {
        headerLines.push("# 额外请求头 JSON 无法解析");
      }
    }
    if (method !== "GET") headerLines.push('-H "Content-Type: application/json"');
    const lines = [`curl "${gatewayBaseURL}${path}" -X ${method}`];
    if (headerLines.length) lines.push(...headerLines);
    if (method !== "GET" && body.trim()) lines.push(`-d '${body}'`);
    return lines.join(" \\\n  ");
  })();

  const upstreamDebugInfo = useMemo(() => parseUpstreamDebugInfo(result), [result]);

  return (
    <div className="space-y-6">
      <section className="rounded-[30px] border border-slate-200/70 bg-white/90 p-8 shadow-sm">
        <div className="max-w-3xl">
          <div className="text-xs uppercase tracking-[0.24em] text-slate-400">接口调试</div>
          <h1 className="mt-3 text-3xl font-semibold tracking-tight text-slate-900">获取全部模型并逐模型测试</h1>
          <p className="mt-3 text-sm leading-7 text-slate-500">
            这里会先载入自定义 API Key，再通过真实网关出口获取模型列表。你可以按模型测试 OpenAI、Claude、Gemini，并查看请求路径、方法、鉴权头、状态码、耗时、JSON 与原始 SSE 文本。
          </p>
        </div>
      </section>

      {(message || error) && (
        <div className={`rounded-2xl border px-5 py-4 text-sm ${error ? "border-rose-200 bg-rose-50 text-rose-700" : "border-emerald-200 bg-emerald-50 text-emerald-700"}`}>
          {error || message}
        </div>
      )}

      <section className="grid gap-6 xl:grid-cols-[0.95fr_1.05fr]">
        <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
          <CardHeader>
            <CardTitle>请求参数</CardTitle>
            <CardDescription>先选自定义 API Key，再获取模型列表，然后按模型测试不同协议。</CardDescription>
          </CardHeader>
          <CardContent className="space-y-5">
            <div className="grid gap-4 md:grid-cols-[1fr_1fr_auto]">
              <label className="space-y-2 block">
                <span className="text-sm text-slate-500">从自定义 API Key 列表载入</span>
                <Select value={selectedMasterKeyId} onChange={(e) => setSelectedMasterKeyId(e.target.value)}>
                  <option value="">手动输入</option>
                  {masterKeys.map((item) => (
                    <option key={item.id} value={String(item.id)}>{item.name} · {item.maskedKey} · {item.status}</option>
                  ))}
                </Select>
              </label>
              <label className="space-y-2 block">
                <span className="text-sm text-slate-500">Token</span>
                <Input value={token} onChange={(e) => setToken(e.target.value)} placeholder="在这里粘贴自定义 API Key" />
              </label>
              <div className="flex items-end gap-3">
                <Button variant="outline" onClick={loadMasterKey} disabled={loadingToken || !selectedMasterKeyId}>{loadingToken ? "载入中..." : "载入 API Key"}</Button>
              </div>
            </div>

            {masterKeysError ? <div className="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">读取自定义 API Key 列表失败，只能手动输入 token。</div> : null}

            <div className="grid gap-4 md:grid-cols-[1fr_1fr_auto]">
              <label className="space-y-2 block">
                <span className="text-sm text-slate-500">模型列表</span>
                <Select value={selectedModel} onChange={(e) => { setSelectedModel(e.target.value); applyRecommendedTemplate(testMode, e.target.value); }}>
                  <option value="">请先获取模型列表</option>
                  {models.map((model, index) => (
                    <option key={`${model}-${index}`} value={model}>{model}</option>
                  ))}
                </Select>
              </label>
              <label className="space-y-2 block">
                <span className="text-sm text-slate-500">测试类型</span>
                <Select value={testMode} onChange={(e) => { const nextMode = e.target.value as TestMode; setTestMode(nextMode); applyRecommendedTemplate(nextMode, selectedModel); }}>
                  {Object.entries(modeLabels).map(([value, label]) => (
                    <option key={value} value={value}>{label}</option>
                  ))}
                </Select>
              </label>
              <div className="flex items-end gap-3">
                <Button variant="outline" onClick={loadModels} disabled={loadingModels || !token.trim()}>{loadingModels ? "获取中..." : "获取模型列表"}</Button>
              </div>
            </div>

            <div className="grid gap-4 md:grid-cols-2">
              <label className="space-y-2 block">
                <span className="text-sm text-slate-500">请求方法</span>
                <Select value={method} onChange={(e) => setMethod(e.target.value as "GET" | "POST") }>
                  <option value="GET">GET</option>
                  <option value="POST">POST</option>
                </Select>
              </label>
              <label className="space-y-2 block">
                <span className="text-sm text-slate-500">鉴权头类型</span>
                <Select value={tokenHeader} onChange={(e) => setTokenHeader(e.target.value as TokenHeader)}>
                  <option value="authorization">Authorization: Bearer</option>
                  <option value="x-api-key">x-api-key</option>
                  <option value="x-goog-api-key">x-goog-api-key</option>
                </Select>
              </label>
            </div>

            <label className="space-y-2 block">
              <span className="text-sm text-slate-500">绝对网关出口</span>
              <Input value={`${gatewayBaseURL}${path}`} disabled />
            </label>

            <label className="space-y-2 block">
              <span className="text-sm text-slate-500">接口路径</span>
              <Input value={path} onChange={(e) => setPath(e.target.value)} placeholder="/v1/chat/completions" />
            </label>

            <label className="space-y-2 block">
              <span className="text-sm text-slate-500">额外请求头（JSON）</span>
              <Textarea value={extraHeadersText} onChange={(e) => setExtraHeadersText(e.target.value)} className="h-28 font-mono text-xs" placeholder='{"anthropic-version":"2023-06-01"}' />
            </label>

            <label className="space-y-2 block">
              <span className="text-sm text-slate-500">请求体</span>
              <Textarea value={body} onChange={(e) => setBody(e.target.value)} className="h-72 font-mono text-xs" placeholder='{"model":"gpt-4o","messages":[...]}' />
            </label>

            <div className="flex flex-wrap gap-3">
              <Button onClick={sendRequest} disabled={sending || !token.trim()}>{sending ? "请求中..." : "发送调试请求"}</Button>
              <Button variant="outline" onClick={runBatchTest} disabled={sending || !token.trim() || !selectedModel}>一键测试当前模型</Button>
              <Button variant="outline" onClick={copyCurlPreview}>{"复制 curl"}</Button>
            </div>
          </CardContent>
        </Card>

        <div className="space-y-6">
          <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
            <CardHeader>
              <CardTitle>curl 预览</CardTitle>
              <CardDescription>这个命令与当前表单内容完全一致，已使用真实绝对地址。</CardDescription>
            </CardHeader>
            <CardContent>
              <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded-2xl border border-slate-200 bg-slate-50 p-4 text-xs text-slate-700">{curlPreview}</pre>
            </CardContent>
          </Card>

          <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
            <CardHeader>
              <CardTitle>当前模型批量测试结果</CardTitle>
              <CardDescription>Chat 模型会跑 chat / responses / claude / gemini；embedding 候选模型会跑 embeddings。</CardDescription>
            </CardHeader>
            <CardContent>
              {batchResults.length === 0 ? (
                <div className="rounded-xl border border-dashed border-slate-200 px-4 py-10 text-center text-sm text-slate-500">执行“一键测试当前模型”后，这里会显示每项请求是否成功，以及每项请求返回的详细结果。</div>
              ) : (
                <div className="space-y-4">
                  {batchResults.map((item, index) => (
                    <div key={`${item.mode}-${index}`} className="rounded-2xl border border-slate-200 bg-slate-50 p-4 text-sm text-slate-700">
                      <div className="flex flex-wrap items-center justify-between gap-3">
                        <div className="font-medium text-slate-900">{item.label}</div>
                        <Badge variant={item.ok ? "default" : "destructive"}>{item.ok ? "成功" : "失败"}</Badge>
                      </div>
                      <div className="mt-2 grid gap-2 md:grid-cols-2 xl:grid-cols-4 text-xs text-slate-500">
                        <div>HTTP {item.status} {item.statusText}</div>
                        <div>耗时 {item.durationMs} ms</div>
                        <div>返回类型 {item.contentType || "(空)"}</div>
                        <div>上游 Key {item.upstream?.keyName || "-"}</div>
                      </div>
                      {item.upstream && (item.upstream.keyName || item.upstream.keyChain || item.upstream.lastError || item.upstream.operation) ? (
                        <div className="mt-3 rounded-xl border border-sky-200 bg-sky-50 p-3 text-xs text-slate-700">
                          <div>上游操作：<span className="font-mono">{item.upstream.operation || "-"}</span></div>
                          <div className="mt-1">切换链路：<span className="font-mono break-all">{item.upstream.keyChain || "-"}</span></div>
                          <div className="mt-1">尝试次数：{item.upstream.attempts} · 是否切换：{item.upstream.switched === "true" ? "是" : "否"}</div>
                          <div className="mt-1">最近失败原因：{item.upstream.lastError || "无"}</div>
                        </div>
                      ) : null}
                      {item.error ? <div className="mt-3 text-xs text-rose-600">错误说明：{item.error}</div> : null}
                      <details className="mt-3">
                        <summary className="cursor-pointer text-xs text-sky-600">展开查看该请求返回内容</summary>
                        <div className="mt-3 space-y-3">
                          <div>
                            <div className="mb-1 text-xs text-slate-500">响应头</div>
                            <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded-xl border border-slate-200 bg-white p-3 text-[11px] text-slate-700">{formatJSON(item.headers)}</pre>
                          </div>
                          <div>
                            <div className="mb-1 text-xs text-slate-500">JSON 视图</div>
                            <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded-xl border border-slate-200 bg-white p-3 text-[11px] text-slate-700">{item.bodyJson ? formatJSON(item.bodyJson) : "不是 JSON，请看原始文本。"}</pre>
                          </div>
                          <div>
                            <div className="mb-1 text-xs text-slate-500">原始文本 / SSE</div>
                            <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded-xl border border-slate-200 bg-white p-3 text-[11px] text-slate-700">{item.bodyText || "(空)"}</pre>
                          </div>
                        </div>
                      </details>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>

          <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
            <CardHeader>
              <CardTitle>响应结果</CardTitle>
              <CardDescription>方便同时查看状态码、头信息、JSON 和原始 SSE 文本。</CardDescription>
            </CardHeader>
            <CardContent>
              {!result ? (
                <div className="rounded-xl border border-dashed border-slate-200 px-4 py-10 text-center text-sm text-slate-500">发送请求后，这里会显示结果。</div>
              ) : (
                <div className="space-y-4">
                  <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                    <Stat label="绝对 URL" value={`${gatewayBaseURL}${path}`} />
                    <Stat label="请求方法" value={method} />
                    <Stat label="鉴权头" value={tokenHeader} />
                    <Stat label="状态" value={`${result.status} ${result.statusText}`} />
                    <Stat label="耗时" value={`${result.durationMs} ms`} />
                    <Stat label="返回类型" value={result.contentType || "(空)"} />
                    <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
                      <div className="text-xs uppercase tracking-[0.2em] text-slate-400">是否成功</div>
                      <div className="mt-2"><Badge variant={result.ok ? "default" : "destructive"}>{result.ok ? "成功" : "失败"}</Badge></div>
                    </div>
                  </div>

                  {upstreamDebugInfo && (upstreamDebugInfo.keyName || upstreamDebugInfo.keyChain || upstreamDebugInfo.lastError || upstreamDebugInfo.operation) ? (
                    <div className="rounded-2xl border border-sky-200 bg-sky-50 p-4 text-sm text-slate-700">
                      <div className="mb-2 text-xs uppercase tracking-[0.2em] text-sky-600">上游诊断</div>
                      <div className="grid gap-3 md:grid-cols-2">
                        <div>上游操作：<span className="font-mono">{upstreamDebugInfo.operation || "-"}</span></div>
                        <div>最终上游 Key：<span className="font-medium text-slate-900">{upstreamDebugInfo.keyName || "-"}</span></div>
                        <div>尝试次数：<span className="font-mono">{upstreamDebugInfo.attempts}</span></div>
                        <div>是否切换：{upstreamDebugInfo.switched === "true" ? "是" : "否"}</div>
                        <div className="md:col-span-2 break-all">切换链路：<span className="font-mono text-xs">{upstreamDebugInfo.keyChain || "-"}</span></div>
                        <div className="md:col-span-2 break-all">最近失败原因：{upstreamDebugInfo.lastError || "无"}</div>
                      </div>
                    </div>
                  ) : null}

                  <div>
                    <div className="mb-2 text-sm text-slate-500">响应头</div>
                    <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded-2xl border border-slate-200 bg-slate-50 p-4 text-xs text-slate-700">{formatJSON(result.headers)}</pre>
                  </div>
                  <div>
                    <div className="mb-2 text-sm text-slate-500">JSON 视图</div>
                    <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded-2xl border border-slate-200 bg-slate-50 p-4 text-xs text-slate-700">{result.bodyJson ? formatJSON(result.bodyJson) : "不是 JSON，请看下方原始文本。"}</pre>
                  </div>
                  <div>
                    <div className="mb-2 text-sm text-slate-500">原始文本 / SSE</div>
                    <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded-2xl border border-slate-200 bg-slate-50 p-4 text-xs text-slate-700">{result.bodyText || "(空)"}</pre>
                  </div>
                  {result.error ? <div className="text-sm text-rose-600">错误说明：{result.error}</div> : null}
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </section>
    </div>
  );
}

function buildTemplate(mode: TestMode, model: string) {
  const selectedModel = model || "meta/llama-3.1-8b-instruct";
  switch (mode) {
    case "models":
      return { method: "GET" as const, path: "/v1/models", tokenHeader: "authorization" as TokenHeader, extraHeadersText: "", body: "" };
    case "chat":
      return {
        method: "POST" as const,
        path: "/v1/chat/completions",
        tokenHeader: "authorization" as TokenHeader,
        extraHeadersText: "",
        body: JSON.stringify({ model: selectedModel, messages: [{ role: "user", content: "你好，请简单介绍一下这个模型。" }], stream: false }, null, 2),
      };
    case "responses":
      return {
        method: "POST" as const,
        path: "/v1/responses",
        tokenHeader: "authorization" as TokenHeader,
        extraHeadersText: "",
        body: JSON.stringify({ model: selectedModel, input: "写一个 Go hello world", stream: false }, null, 2),
      };
    case "embeddings":
      return {
        method: "POST" as const,
        path: "/v1/embeddings",
        tokenHeader: "authorization" as TokenHeader,
        extraHeadersText: "",
        body: JSON.stringify({ model: selectedModel, input: ["NVIDIA", "OpenAI compatibility"] }, null, 2),
      };
    case "claude":
      return {
        method: "POST" as const,
        path: "/anthropic/v1/messages",
        tokenHeader: "x-api-key" as TokenHeader,
        extraHeadersText: JSON.stringify({ "anthropic-version": "2023-06-01" }, null, 2),
        body: JSON.stringify({ model: selectedModel, max_tokens: 256, stream: false, messages: [{ role: "user", content: "你好" }] }, null, 2),
      };
    case "gemini_generate":
      return {
        method: "POST" as const,
        path: `/v1beta/models/${encodeURIComponent(selectedModel)}:generateContent`,
        tokenHeader: "x-goog-api-key" as TokenHeader,
        extraHeadersText: "",
        body: JSON.stringify({ contents: [{ role: "user", parts: [{ text: "你好" }] }] }, null, 2),
      };
    case "gemini_stream":
      return {
        method: "POST" as const,
        path: `/v1beta/models/${encodeURIComponent(selectedModel)}:streamGenerateContent?alt=sse`,
        tokenHeader: "x-goog-api-key" as TokenHeader,
        extraHeadersText: "",
        body: JSON.stringify({ contents: [{ role: "user", parts: [{ text: "请流式回复一句话" }] }] }, null, 2),
      };
  }
}

function isEmbeddingCandidate(model: string) {
  const lower = model.toLowerCase();
  return lower.includes("embed") || lower.includes("embedding") || lower.includes("e5") || lower.includes("bge") || lower.includes("rerank");
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
      <div className="text-xs uppercase tracking-[0.2em] text-slate-400">{label}</div>
      <div className="mt-2 break-all font-mono text-sm text-slate-800">{value}</div>
    </div>
  );
}

function formatJSON(value: unknown) {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}
