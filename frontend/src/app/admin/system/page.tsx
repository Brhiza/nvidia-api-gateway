"use client";

import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";

interface SystemConfig {
  upstreamBaseURL: string;
  schedulerStrategy: string;
  maxRetries: number;
  maxConcurrency: number;
  requestTimeoutSecond: number;
  upstreamProxyURL: string;
  gatewayBaseURL: string;
  firstByteTimeoutMs: number;
  healthProbeTimeoutSecond: number;
  enableOpenAI: boolean;
  enableClaude: boolean;
  enableGemini: boolean;
  anonymousAccess: boolean;
}

const defaultConfig: SystemConfig = {
  upstreamBaseURL: "https://integrate.api.nvidia.com/v1",
  schedulerStrategy: "weighted_round_robin",
  maxRetries: 5,
  maxConcurrency: 3,
  requestTimeoutSecond: 600,
  upstreamProxyURL: "",
  gatewayBaseURL: "http://127.0.0.1:18080",
  firstByteTimeoutMs: 90000,
  healthProbeTimeoutSecond: 45,
  enableOpenAI: true,
  enableClaude: true,
  enableGemini: true,
  anonymousAccess: false,
};

export default function SystemConfigPage() {
  const [config, setConfig] = useState<SystemConfig>(defaultConfig);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const load = async () => {
      try {
        const res = await fetch("/api/system/config", { cache: "no-store" });
        const data = await res.json().catch(() => null);
        if (!res.ok) {
          setError(data?.error || "读取系统设置失败。");
          return;
        }
        setConfig({ ...defaultConfig, ...data });
      } catch {
        setError("读取系统设置失败。");
      } finally {
        setLoading(false);
      }
    };
    void load();
  }, []);

  const updateField = <K extends keyof SystemConfig>(key: K, value: SystemConfig[K]) => {
    setConfig((current) => ({ ...current, [key]: value }));
  };

  const save = async () => {
    setSaving(true);
    setMessage(null);
    setError(null);
    try {
      const res = await fetch("/api/system/config", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(config),
      });
      const data = await res.json().catch(() => null);
      if (!res.ok) {
        setError(data?.error || "保存失败。");
        return;
      }
      setMessage(data?.message || "系统设置已保存。");
      if (data?.config) {
        setConfig({ ...defaultConfig, ...data.config });
      }
    } catch {
      setError("保存失败。");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-6">
      <section className="rounded-[30px] border border-slate-200/70 bg-white/90 p-8 shadow-sm">
        <div className="max-w-3xl">
          <div className="text-xs uppercase tracking-[0.24em] text-slate-400">系统设置</div>
          <h1 className="mt-3 text-3xl font-semibold tracking-tight text-slate-900">调整网关运行参数</h1>
          <p className="mt-3 text-sm leading-7 text-slate-500">
            {"这里可以设置上游地址、代理、重试次数、并发数、首包切换超时、健康探测超时和协议开关。保存后会立即生效；业务请求和健康检查都会共用这套上游代理。"}
          </p>
        </div>
      </section>

      {(message || error) && (
        <div className={`rounded-2xl border px-5 py-4 text-sm ${error ? "border-rose-200 bg-rose-50 text-rose-700" : "border-emerald-200 bg-emerald-50 text-emerald-700"}`}>
          {error || message}
        </div>
      )}

      {config.anonymousAccess ? (
        <div className="rounded-2xl border border-amber-200 bg-amber-50 px-5 py-4 text-sm leading-7 text-amber-800">
          当前实例仍允许匿名访问（旧配置保留）。建议创建自定义 API Key 后关闭匿名访问。
        </div>
      ) : null}

      <section className="grid gap-6 xl:grid-cols-2">
        <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
          <CardHeader>
            <CardTitle>基础参数</CardTitle>
            <CardDescription>这些设置决定网关如何访问上游和调度请求。</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <label className="space-y-2 block">
              <span className="text-sm text-slate-500">真实网关出口地址</span>
              <Input value={config.gatewayBaseURL} disabled />
            </label>
            <label className="space-y-2 block">
              <span className="text-sm text-slate-500">上游地址</span>
              <Input value={config.upstreamBaseURL} onChange={(e) => updateField("upstreamBaseURL", e.target.value)} placeholder="https://integrate.api.nvidia.com/v1" disabled={loading} />
            </label>
            <label className="space-y-2 block">
              <span className="text-sm text-slate-500">上游代理</span>
              <Input
                value={config.upstreamProxyURL}
                onChange={(e) => updateField("upstreamProxyURL", e.target.value)}
                placeholder="留空=继承环境变量；direct=直连；http://127.0.0.1:7890；socks5h://user:pass@127.0.0.1:1080"
                disabled={loading}
              />
              <p className="text-xs leading-6 text-slate-500">
                支持 <code>http://</code>、<code>https://</code>、<code>socks5://</code>、<code>socks5h://</code>。留空时按系统环境变量（HTTP_PROXY / HTTPS_PROXY / ALL_PROXY）处理；填 <code>direct</code> 或 <code>none</code> 表示强制直连。
              </p>
            </label>
            <div className="grid gap-4 md:grid-cols-2">
              <label className="space-y-2 block">
                <span className="text-sm text-slate-500">调度策略</span>
                <Input value={config.schedulerStrategy} onChange={(e) => updateField("schedulerStrategy", e.target.value)} disabled={loading} />
              </label>
              <label className="space-y-2 block">
                <span className="text-sm text-slate-500">首包切换超时（毫秒）</span>
                <Input type="number" min="500" value={String(config.firstByteTimeoutMs)} onChange={(e) => updateField("firstByteTimeoutMs", Number(e.target.value))} disabled={loading} />
              </label>
            </div>
            <div className="grid gap-4 md:grid-cols-4">
              <label className="space-y-2 block">
                <span className="text-sm text-slate-500">最大重试</span>
                <Input type="number" min="1" value={String(config.maxRetries)} onChange={(e) => updateField("maxRetries", Number(e.target.value))} disabled={loading} />
              </label>
              <label className="space-y-2 block">
                <span className="text-sm text-slate-500">单 key 并发</span>
                <Input type="number" min="1" value={String(config.maxConcurrency)} onChange={(e) => updateField("maxConcurrency", Number(e.target.value))} disabled={loading} />
              </label>
              <label className="space-y-2 block">
                <span className="text-sm text-slate-500">请求超时（秒）</span>
                <Input type="number" min="30" value={String(config.requestTimeoutSecond)} onChange={(e) => updateField("requestTimeoutSecond", Number(e.target.value))} disabled={loading} />
              </label>
              <label className="space-y-2 block">
                <span className="text-sm text-slate-500">{"健康探测超时（秒）"}</span>
                <Input type="number" min="5" value={String(config.healthProbeTimeoutSecond)} onChange={(e) => updateField("healthProbeTimeoutSecond", Number(e.target.value))} disabled={loading} />
              </label>
            </div>
          </CardContent>
        </Card>

        <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
          <CardHeader>
            <CardTitle>协议与访问控制</CardTitle>
            <CardDescription>决定哪些协议开放，以及是否允许匿名访问。</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <Toggle label="启用 OpenAI 出口" checked={config.enableOpenAI} onChange={(value) => updateField("enableOpenAI", value)} />
            <Toggle label="启用 Claude 出口" checked={config.enableClaude} onChange={(value) => updateField("enableClaude", value)} />
            <Toggle label="启用 Gemini 出口" checked={config.enableGemini} onChange={(value) => updateField("enableGemini", value)} />
            <Toggle label="允许匿名访问（没有自定义 API Key 时也能调用）" checked={config.anonymousAccess} onChange={(value) => updateField("anonymousAccess", value)} />
            <div className="rounded-2xl border border-dashed border-slate-200 bg-slate-50 px-4 py-4 text-sm text-slate-500">
              <div>录入上游 key 时，直接粘贴你拿到的完整值即可，例如：</div>
              <div className="mt-2 break-all font-mono text-slate-700">nvapi-fwRasMdcYM2U*******************************yfebnDf</div>
            </div>
          </CardContent>
        </Card>
      </section>

      <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
        <CardHeader>
          <CardTitle>常用接口路径</CardTitle>
          <CardDescription>便于核对不同客户端应该走哪条路由。</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 text-sm text-slate-600 md:grid-cols-2 xl:grid-cols-3">
          <PathItem label="OpenAI 模型列表" value="/v1/models" />
          <PathItem label="OpenAI 对话" value="/v1/chat/completions" />
          <PathItem label="OpenAI Responses" value="/v1/responses" />
          <PathItem label="OpenAI Embeddings" value="/v1/embeddings" />
          <PathItem label="Claude Messages" value="/anthropic/v1/messages" />
          <PathItem label="Gemini Stream" value="/v1beta/models/{model}:streamGenerateContent" />
        </CardContent>
      </Card>

      <div className="flex gap-3">
        <Button onClick={save} disabled={loading || saving}>{saving ? "保存中..." : "保存设置"}</Button>
      </div>
    </div>
  );
}

function Toggle({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <label className="flex items-center justify-between rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-700">
      <span>{label}</span>
      <Switch checked={checked} onCheckedChange={onChange} />
    </label>
  );
}

function PathItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3">
      <div className="text-xs uppercase tracking-[0.2em] text-slate-400">{label}</div>
      <div className="mt-2 break-all font-mono text-slate-800">{value}</div>
    </div>
  );
}
