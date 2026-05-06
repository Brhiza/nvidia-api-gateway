"use client";

import { useEffect, useMemo, useState } from "react";
import useSWR from "swr";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { copyToClipboard } from "@/lib/clipboard";

const fetcher = async (url: string) => {
  const res = await fetch(url, { cache: "no-store" });
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error(data?.error || "request_failed");
  }
  return data;
};

interface MasterKey {
  id: number;
  name: string;
  maskedKey: string;
  rpm: number;
  tpm: number;
  quota: number;
  usedQuota: number;
  status: string;
  createdAt: string;
  updatedAt: string;
}

interface MasterKeyResponse {
  keys: MasterKey[];
}

interface SystemConfig {
  gatewayBaseURL: string;
  anonymousAccess: boolean;
}

interface MasterKeyFormState {
  name: string;
  key: string;
  rpm: string;
  tpm: string;
  quota: string;
  rpmUnlimited: boolean;
  tpmUnlimited: boolean;
  quotaUnlimited: boolean;
}

const emptyForm: MasterKeyFormState = {
  name: "",
  key: "",
  rpm: "",
  tpm: "",
  quota: "",
  rpmUnlimited: true,
  tpmUnlimited: true,
  quotaUnlimited: true,
};

function buildPayloadFromForm(form: MasterKeyFormState) {
  return {
    name: form.name.trim(),
    key: form.key.trim(),
    rpm: form.rpmUnlimited ? 0 : Number(form.rpm),
    tpm: form.tpmUnlimited ? 0 : Number(form.tpm),
    quota: form.quotaUnlimited ? -1 : Number(form.quota),
  };
}

function buildEditForm(key: MasterKey): MasterKeyFormState {
  return {
    name: key.name,
    key: "",
    rpm: key.rpm > 0 ? String(key.rpm) : "",
    tpm: key.tpm > 0 ? String(key.tpm) : "",
    quota: key.quota >= 0 ? String(key.quota) : "",
    rpmUnlimited: key.rpm === 0,
    tpmUnlimited: key.tpm === 0,
    quotaUnlimited: key.quota === -1,
  };
}

function formatRPM(value: number) {
  return value === 0 ? "不限" : String(value);
}

function formatTPM(value: number) {
  return value === 0 ? "不限" : String(value);
}

function formatQuota(value: number) {
  return value === -1 ? "不限" : String(value);
}

export default function MasterKeysPage() {
  const { data, error, mutate } = useSWR<MasterKeyResponse>("/api/master-keys", fetcher, { refreshInterval: 5000 });
  const { data: systemConfig } = useSWR<SystemConfig>("/api/system/config", fetcher);

  const keys = useMemo(() => [...(data?.keys ?? [])].sort((a, b) => a.id - b.id), [data?.keys]);
  const gatewayBaseURL = systemConfig?.gatewayBaseURL || "http://127.0.0.1:18080";

  const [createForm, setCreateForm] = useState<MasterKeyFormState>(() => ({ ...emptyForm }));
  const [editForm, setEditForm] = useState<MasterKeyFormState>(() => ({ ...emptyForm }));
  const [editingId, setEditingId] = useState<number | null>(null);
  const [busyId, setBusyId] = useState<number | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [plainKeyNotice, setPlainKeyNotice] = useState<string | null>(null);
  const [exampleToken, setExampleToken] = useState<string>("sk-<your-custom-api-key>");

  useEffect(() => {
    if (!message && !errorMessage) return;
    const timer = window.setTimeout(() => {
      setMessage(null);
      setErrorMessage(null);
    }, 4500);
    return () => window.clearTimeout(timer);
  }, [message, errorMessage]);

  const resetMessages = () => {
    setMessage(null);
    setErrorMessage(null);
  };

  const setVisibleToken = (token: string | null) => {
    setPlainKeyNotice(token);
    if (token) setExampleToken(token);
  };

  const copyText = async (text: string, okMessage: string) => {
    try {
      const copied = await copyToClipboard(text);
      if (!copied) throw new Error("copy_failed");
      setMessage(okMessage);
    } catch {
      setErrorMessage("复制失败，请手动复制。");
    }
  };

  const validateForm = (form: MasterKeyFormState) => {
    if (!form.name.trim()) return "名称不能为空。";

    if (!form.rpmUnlimited) {
      if (!form.rpm.trim()) return "关闭 RPM 不限后，请填写大于 0 的值。";
      const rpm = Number(form.rpm);
      if (!Number.isInteger(rpm) || rpm <= 0) return "RPM 必须是大于 0 的整数。";
    }

    if (!form.tpmUnlimited) {
      if (!form.tpm.trim()) return "关闭 TPM 不限后，请填写大于 0 的值。";
      const tpm = Number(form.tpm);
      if (!Number.isInteger(tpm) || tpm <= 0) return "TPM 必须是大于 0 的整数。";
    }

    if (!form.quotaUnlimited) {
      if (!form.quota.trim()) return "关闭配额不限后，请填写 0 或更大的数字。";
      const quota = Number(form.quota);
      if (!Number.isInteger(quota) || quota < 0) return "配额必须是 0 或更大的整数。";
    }

    return null;
  };

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    resetMessages();
    setVisibleToken(null);
    const validation = validateForm(createForm);
    if (validation) {
      setErrorMessage(validation);
      return;
    }
    setIsSubmitting(true);
    try {
      const res = await fetch("/api/master-keys", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          ...buildPayloadFromForm(createForm),
          status: "Active",
        }),
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "创建自定义 API Key 失败。");
        return;
      }
      setCreateForm({ ...emptyForm });
      setMessage(payload?.message || "自定义 API Key 已创建。");
      setVisibleToken(payload?.plainKey ?? null);
      await mutate();
    } finally {
      setIsSubmitting(false);
    }
  };

  const startEdit = (key: MasterKey) => {
    resetMessages();
    setVisibleToken(null);
    setEditingId(key.id);
    setEditForm(buildEditForm(key));
  };

  const cancelEdit = () => {
    setEditingId(null);
    setEditForm({ ...emptyForm });
  };

  const submitEdit = async (id: number) => {
    resetMessages();
    setVisibleToken(null);
    const validation = validateForm(editForm);
    if (validation) {
      setErrorMessage(validation);
      return;
    }
    setBusyId(id);
    try {
      const res = await fetch(`/api/master-keys/${id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(buildPayloadFromForm(editForm)),
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "保存失败。");
        return;
      }
      setMessage(payload?.message || "自定义 API Key 已更新。");
      setVisibleToken(payload?.plainKey ?? null);
      cancelEdit();
      await mutate();
    } finally {
      setBusyId(null);
    }
  };

  const updateStatus = async (id: number, status: "Active" | "Disabled") => {
    resetMessages();
    setBusyId(id);
    try {
      const res = await fetch(`/api/master-keys/${id}/status`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status }),
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "更新状态失败。");
        return;
      }
      setMessage(payload?.message || "状态已更新。");
      await mutate();
    } finally {
      setBusyId(null);
    }
  };

  const revealAndCopyKey = async (id: number) => {
    resetMessages();
    setBusyId(id);
    try {
      const res = await fetch(`/api/master-keys/${id}/reveal`, { method: "POST" });
      const payload = await res.json().catch(() => null);
      if (!res.ok || !payload?.plainKey) {
        setErrorMessage(payload?.error || "读取明文 API Key 失败。");
        return;
      }
      setVisibleToken(payload.plainKey);
      await copyText(payload.plainKey, `已复制 ${payload?.name || "API Key"}`);
    } finally {
      setBusyId(null);
    }
  };

  const rotateKey = async (id: number) => {
    resetMessages();
    setVisibleToken(null);
    if (!window.confirm(`确定要轮换自定义 API Key #${id.toString().padStart(4, "0")} 吗？旧 Key 会马上失效。`)) {
      return;
    }
    setBusyId(id);
    try {
      const res = await fetch(`/api/master-keys/${id}/rotate`, { method: "POST" });
      const payload = await res.json().catch(() => null);
      if (!res.ok || !payload?.plainKey) {
        setErrorMessage(payload?.error || "轮换自定义 API Key 失败。");
        return;
      }
      setVisibleToken(payload.plainKey);
      setMessage(payload?.message || "自定义 API Key 已轮换。");
      await mutate();
    } finally {
      setBusyId(null);
    }
  };

  const deleteKey = async (id: number) => {
    resetMessages();
    if (!window.confirm(`确定要删除自定义 API Key #${id.toString().padStart(4, "0")} 吗？`)) return;
    setBusyId(id);
    try {
      const res = await fetch(`/api/master-keys/${id}`, { method: "DELETE" });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "删除失败。");
        return;
      }
      setMessage(payload?.message || "自定义 API Key 已删除。");
      if (editingId === id) cancelEdit();
      await mutate();
    } finally {
      setBusyId(null);
    }
  };

  const curlExamples = {
    models: `curl ${gatewayBaseURL}/v1/models \\
  -H "Authorization: Bearer ${exampleToken}"`,
    openai: `curl ${gatewayBaseURL}/v1/chat/completions \\
  -H "Authorization: Bearer ${exampleToken}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "你好，请简单介绍一下这个网关。"}],
    "stream": false
  }'`,
    responses: `curl ${gatewayBaseURL}/v1/responses \\
  -H "Authorization: Bearer ${exampleToken}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "gpt-4o",
    "input": "写一个 Go hello world",
    "stream": false
  }'`,
    embeddings: `curl ${gatewayBaseURL}/v1/embeddings \\
  -H "Authorization: Bearer ${exampleToken}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "text-embedding-3-small",
    "input": ["NVIDIA", "OpenAI compatibility"]
  }'`,
    claude: `curl ${gatewayBaseURL}/anthropic/v1/messages \\
  -H "x-api-key: ${exampleToken}" \\
  -H "anthropic-version: 2023-06-01" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "claude-sonnet-4-6",
    "max_tokens": 1024,
    "stream": false,
    "messages": [{"role": "user", "content": "你好"}]
  }'`,
    gemini: `curl "${gatewayBaseURL}/v1beta/models/gemini-2.5-pro:streamGenerateContent?alt=sse" \\
  -H "x-goog-api-key: ${exampleToken}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "contents": [
      {
        "role": "user",
        "parts": [{"text": "你好"}]
      }
    ]
  }'`,
  };

  return (
    <div className="space-y-6">
      <section className="rounded-[30px] border border-slate-200/70 bg-white/90 p-8 shadow-sm">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <div className="text-xs uppercase tracking-[0.24em] text-slate-400">自定义 API Key</div>
            <h1 className="mt-3 text-3xl font-semibold tracking-tight text-slate-900">给下游系统分配网关 API Key</h1>
            <p className="mt-3 max-w-3xl text-sm leading-7 text-slate-500">
              这里管理的是下游访问网关时要填写的自定义 API Key。给 OpenAI 客户端填写的 api_key，就是这里创建的自定义 API Key；base_url 请填 {gatewayBaseURL}/v1。
            </p>
          </div>
          <div className="rounded-full border border-slate-200 bg-slate-50 px-4 py-2 text-sm text-slate-600">
            当前 API Key 数量：{keys.length}
          </div>
        </div>
      </section>

      {keys.length === 0 && systemConfig && !systemConfig.anonymousAccess ? (
        <div className="rounded-2xl border border-rose-200 bg-rose-50 px-5 py-4 text-sm leading-7 text-rose-700">
          当前未开启匿名访问，且还没有任何自定义 API Key。请先创建一个 key，再去调用 /v1/models 或 /v1/chat/completions。
        </div>
      ) : null}

      {(message || errorMessage) && (
        <div className={`rounded-2xl border px-5 py-4 text-sm ${errorMessage ? "border-rose-200 bg-rose-50 text-rose-700" : "border-emerald-200 bg-emerald-50 text-emerald-700"}`}>
          {errorMessage || message}
        </div>
      )}

      {plainKeyNotice ? (
        <Card className="border border-amber-200 bg-amber-50/80 shadow-sm">
          <CardHeader>
            <CardTitle>请先保存这个新的自定义 API Key</CardTitle>
            <CardDescription>出于安全考虑，明文 API Key 只会在这里显示一次。</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="rounded-2xl border border-amber-200 bg-white px-4 py-3 font-mono text-sm text-slate-900">{plainKeyNotice}</div>
            <Button onClick={() => copyText(plainKeyNotice, "已复制最新自定义 API Key")}>复制 API Key</Button>
          </CardContent>
        </Card>
      ) : null}

      <section className="grid gap-6 xl:grid-cols-[0.9fr_1.1fr]">
        <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
          <CardHeader>
            <CardTitle>新建自定义 API Key</CardTitle>
            <CardDescription>默认三项都不限；关闭“不限”后，再填写具体数值。</CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="space-y-4">
              <label className="block space-y-2">
                <span className="text-sm text-slate-500">名称</span>
                <Input value={createForm.name} onChange={(e) => setCreateForm({ ...createForm, name: e.target.value })} placeholder="例如：内部工具" />
              </label>
              <label className="block space-y-2">
                <span className="text-sm text-slate-500">自定义 Key（可选）</span>
                <Input value={createForm.key} onChange={(e) => setCreateForm({ ...createForm, key: e.target.value })} placeholder="留空则自动生成" />
              </label>

              <LimitField
                label="RPM"
                description="每分钟最多允许多少次请求。"
                unlimited={createForm.rpmUnlimited}
                onUnlimitedChange={(checked) => setCreateForm({ ...createForm, rpmUnlimited: checked, rpm: checked ? "" : createForm.rpm })}
                value={createForm.rpm}
                onValueChange={(value) => setCreateForm({ ...createForm, rpm: value })}
                placeholder="例如 60"
              />

              <LimitField
                label="TPM"
                description="每分钟最多允许消耗多少 token。"
                unlimited={createForm.tpmUnlimited}
                onUnlimitedChange={(checked) => setCreateForm({ ...createForm, tpmUnlimited: checked, tpm: checked ? "" : createForm.tpm })}
                value={createForm.tpm}
                onValueChange={(value) => setCreateForm({ ...createForm, tpm: value })}
                placeholder="例如 120000"
              />

              <LimitField
                label="总配额"
                description="整个生命周期内最多允许消耗多少 token。"
                unlimited={createForm.quotaUnlimited}
                onUnlimitedChange={(checked) => setCreateForm({ ...createForm, quotaUnlimited: checked, quota: checked ? "" : createForm.quota })}
                value={createForm.quota}
                onValueChange={(value) => setCreateForm({ ...createForm, quota: value })}
                placeholder="例如 500000"
              />

              <div className="flex gap-3">
                <Button type="submit" disabled={isSubmitting}>{isSubmitting ? "创建中..." : "创建 API Key"}</Button>
              </div>
            </form>
          </CardContent>
        </Card>

        <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
          <CardHeader>
            <CardTitle>调用示例</CardTitle>
            <CardDescription>下面这些命令会直接打到真实网关出口：{gatewayBaseURL}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="rounded-2xl border border-sky-200 bg-sky-50 px-4 py-4 text-sm leading-7 text-sky-800">
              <div>1. 先用 <span className="font-mono">GET {gatewayBaseURL}/v1/models</span> 验证鉴权是否成功。</div>
              <div>2. 再用 <span className="font-mono">POST {gatewayBaseURL}/v1/chat/completions</span> 验证模型调用是否成功。</div>
            </div>
            {([
              ["获取模型列表", curlExamples.models],
              ["OpenAI Chat", curlExamples.openai],
              ["OpenAI Responses", curlExamples.responses],
              ["OpenAI Embeddings", curlExamples.embeddings],
              ["Claude", curlExamples.claude],
              ["Gemini", curlExamples.gemini],
            ] as const).map(([label, snippet]) => (
              <div key={label} className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
                <div className="mb-2 flex items-center justify-between gap-3">
                  <div className="text-sm font-medium text-slate-800">{label}</div>
                  <Button variant="outline" size="sm" onClick={() => copyText(snippet, `已复制 ${label} 示例命令`)}>
                    复制
                  </Button>
                </div>
                <pre className="overflow-x-auto whitespace-pre-wrap break-all text-xs text-slate-600">{snippet}</pre>
              </div>
            ))}
          </CardContent>
        </Card>
      </section>

      <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
        <CardHeader>
          <CardTitle>自定义 API Key 列表</CardTitle>
          <CardDescription>可以复制、轮换、编辑、启用或删除现有的网关 API Key。</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {error ? <div className="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">读取自定义 API Key 列表失败，请稍后重试。</div> : null}
          {keys.length === 0 ? (
            <div className="rounded-xl border border-dashed border-slate-200 px-4 py-10 text-center text-sm text-slate-500">当前还没有自定义 API Key。</div>
          ) : (
            <div className="space-y-4">
              {keys.map((key) => {
                const isEditing = editingId === key.id;
                const isBusy = busyId === key.id;
                return (
                  <div key={key.id} className="rounded-2xl border border-slate-200 bg-slate-50/80 p-4">
                    <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
                      <div>
                        <div className="flex flex-wrap items-center gap-3">
                          <div className="text-lg font-semibold text-slate-900">{key.name}</div>
                          <Badge variant={key.status === "Active" ? "default" : "outline"}>{key.status === "Active" ? "启用中" : "已停用"}</Badge>
                          <span className="rounded-full border border-slate-200 bg-white px-3 py-1 text-xs text-slate-500">#{String(key.id).padStart(4, "0")}</span>
                        </div>
                        <div className="mt-3 grid gap-2 text-sm text-slate-500 md:grid-cols-2 xl:grid-cols-4">
                          <div>Key 预览：<span className="font-mono text-slate-700">{key.maskedKey}</span></div>
                          <div>RPM：{formatRPM(key.rpm)}</div>
                          <div>TPM：{formatTPM(key.tpm)}</div>
                          <div>配额：{formatQuota(key.quota)}</div>
                          <div>已用：{key.usedQuota}</div>
                          <div>创建时间：{new Date(key.createdAt).toLocaleString()}</div>
                          <div className="md:col-span-2">更新时间：{new Date(key.updatedAt).toLocaleString()}</div>
                        </div>
                      </div>
                      <div className="flex flex-wrap gap-2 xl:max-w-sm xl:justify-end">
                        <Button variant="outline" size="sm" onClick={() => revealAndCopyKey(key.id)} disabled={isBusy}>复制 API Key</Button>
                        <Button variant="outline" size="sm" onClick={() => rotateKey(key.id)} disabled={isBusy}>轮换 API Key</Button>
                        <Button variant="outline" size="sm" onClick={() => (isEditing ? cancelEdit() : startEdit(key))} disabled={isBusy}>{isEditing ? "收起编辑" : "编辑"}</Button>
                        <Button variant="outline" size="sm" onClick={() => updateStatus(key.id, key.status === "Disabled" ? "Active" : "Disabled")} disabled={isBusy}>{key.status === "Disabled" ? "启用" : "禁用"}</Button>
                        <Button variant="destructive" size="sm" onClick={() => deleteKey(key.id)} disabled={isBusy}>删除</Button>
                      </div>
                    </div>

                    {isEditing ? (
                      <div className="mt-4 space-y-4 rounded-2xl border border-slate-200 bg-white p-4">
                        <label className="block space-y-2">
                          <span className="text-sm text-slate-500">名称</span>
                          <Input value={editForm.name} onChange={(e) => setEditForm({ ...editForm, name: e.target.value })} placeholder="名称" />
                        </label>
                        <label className="block space-y-2">
                          <span className="text-sm text-slate-500">自定义 Key（可选）</span>
                          <Input value={editForm.key} onChange={(e) => setEditForm({ ...editForm, key: e.target.value })} placeholder="留空则不改 API Key" />
                        </label>

                        <LimitField
                          label="RPM"
                          description="每分钟最多允许多少次请求。"
                          unlimited={editForm.rpmUnlimited}
                          onUnlimitedChange={(checked) => setEditForm({ ...editForm, rpmUnlimited: checked, rpm: checked ? "" : editForm.rpm })}
                          value={editForm.rpm}
                          onValueChange={(value) => setEditForm({ ...editForm, rpm: value })}
                          placeholder="例如 60"
                        />

                        <LimitField
                          label="TPM"
                          description="每分钟最多允许消耗多少 token。"
                          unlimited={editForm.tpmUnlimited}
                          onUnlimitedChange={(checked) => setEditForm({ ...editForm, tpmUnlimited: checked, tpm: checked ? "" : editForm.tpm })}
                          value={editForm.tpm}
                          onValueChange={(value) => setEditForm({ ...editForm, tpm: value })}
                          placeholder="例如 120000"
                        />

                        <LimitField
                          label="总配额"
                          description="整个生命周期内最多允许消耗多少 token。"
                          unlimited={editForm.quotaUnlimited}
                          onUnlimitedChange={(checked) => setEditForm({ ...editForm, quotaUnlimited: checked, quota: checked ? "" : editForm.quota })}
                          value={editForm.quota}
                          onValueChange={(value) => setEditForm({ ...editForm, quota: value })}
                          placeholder="例如 500000"
                        />

                        <div className="flex gap-3">
                          <Button size="sm" onClick={() => submitEdit(key.id)} disabled={isBusy}>保存</Button>
                          <Button variant="outline" size="sm" onClick={cancelEdit}>取消</Button>
                        </div>
                      </div>
                    ) : null}
                  </div>
                );
              })}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function LimitField({
  label,
  description,
  unlimited,
  onUnlimitedChange,
  value,
  onValueChange,
  placeholder,
}: {
  label: string;
  description: string;
  unlimited: boolean;
  onUnlimitedChange: (checked: boolean) => void;
  value: string;
  onValueChange: (value: string) => void;
  placeholder: string;
}) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <div className="text-sm font-medium text-slate-800">{label}</div>
          <div className="mt-1 text-xs leading-6 text-slate-500">{description}</div>
        </div>
        <div className="flex items-center gap-3 text-sm text-slate-600">
          <span>{unlimited ? "不限" : "自定义"}</span>
          <Switch checked={unlimited} onCheckedChange={onUnlimitedChange} />
        </div>
      </div>
      <div className="mt-4 space-y-2">
        <Input
          type="number"
          min="0"
          value={unlimited ? "" : value}
          onChange={(e) => onValueChange(e.target.value)}
          placeholder={placeholder}
          disabled={unlimited}
        />
        <div className="text-xs text-slate-500">
          {unlimited ? `当前 ${label} 不受限制。` : `请输入 ${label} 的具体值。`}
        </div>
      </div>
    </div>
  );
}
