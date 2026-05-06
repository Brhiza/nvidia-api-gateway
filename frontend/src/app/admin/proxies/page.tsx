"use client";

import { useEffect, useMemo, useState } from "react";
import useSWR from "swr";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";

const fetcher = async (url: string) => {
  const res = await fetch(url);
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error(data?.error || "request_failed");
  }
  return data;
};

interface ProxyTestRecord {
  success: boolean;
  statusCode?: number;
  responseTime?: number;
  message?: string;
  target?: string;
  testedAt: string;
  summary: string;
}

interface UpstreamProxy {
  id: number;
  name: string;
  group?: string;
  type: string;
  status: string;
  host: string;
  port: number;
  username?: string;
  hasPassword: boolean;
  boundKeyCount: number;
  urlPreview: string;
  lastTest?: ProxyTestRecord | null;
  testHistory?: ProxyTestRecord[];
  createdAt: string;
  updatedAt: string;
}

interface ProxyListResponse {
  proxies: UpstreamProxy[];
}

interface ProxyFormState {
  name: string;
  group: string;
  type: string;
  host: string;
  port: string;
  username: string;
  password: string;
}

interface ProxyTestResult {
  success?: boolean;
  status_code?: number;
  response_time?: number;
  target?: string;
  message?: string;
}

const emptyForm: ProxyFormState = {
  name: "",
  group: "",
  type: "http",
  host: "",
  port: "",
  username: "",
  password: "",
};

export default function ProxiesPage() {
  const { data, error, mutate } = useSWR<ProxyListResponse>("/api/proxies", fetcher, { refreshInterval: 5000 });
  const [groupFilter, setGroupFilter] = useState<string>("");
  const [createForm, setCreateForm] = useState<ProxyFormState>(emptyForm);
  const [editingProxyId, setEditingProxyId] = useState<number | null>(null);
  const [editForm, setEditForm] = useState<ProxyFormState>(emptyForm);
  const [busyProxyId, setBusyProxyId] = useState<number | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<ProxyTestResult | null>(null);

  useEffect(() => {
    if (!message && !errorMessage) return;
    const timer = window.setTimeout(() => {
      setMessage(null);
      setErrorMessage(null);
    }, 4000);
    return () => window.clearTimeout(timer);
  }, [message, errorMessage]);

  const proxyGroups = useMemo(() => {
    const seen = new Set<string>();
    for (const proxy of data?.proxies ?? []) {
      if (!proxy.group?.trim()) continue;
      seen.add(proxy.group.trim());
    }
    return Array.from(seen).sort((a, b) => a.localeCompare(b));
  }, [data?.proxies]);

  const proxies = useMemo(() => {
    const rank = (proxy: UpstreamProxy) => {
      if (proxy.status === "Disabled") return 3;
      if (!proxy.lastTest) return 2;
      return proxy.lastTest.success ? 0 : 1;
    };
    const latency = (proxy: UpstreamProxy) => proxy.lastTest?.responseTime ?? Number.MAX_SAFE_INTEGER;
    const testedAt = (proxy: UpstreamProxy) => (proxy.lastTest?.testedAt ? new Date(proxy.lastTest.testedAt).getTime() : 0);
    return [...(data?.proxies ?? [])]
      .filter((proxy) => !groupFilter || (proxy.group ?? "") === groupFilter)
      .sort((a, b) => {
        if (rank(a) !== rank(b)) return rank(a) - rank(b);
        if (latency(a) !== latency(b)) return latency(a) - latency(b);
        if (testedAt(a) !== testedAt(b)) return testedAt(b) - testedAt(a);
        if ((a.group ?? "") !== (b.group ?? "")) return (a.group ?? "").localeCompare(b.group ?? "");
        return a.name.localeCompare(b.name);
      });
  }, [data?.proxies, groupFilter]);

  const resetMessages = () => {
    setMessage(null);
    setErrorMessage(null);
    setTestResult(null);
  };

  const reload = async () => {
    await mutate();
  };

  const validate = (form: ProxyFormState) => {
    if (!form.name.trim()) return "代理名称不能为空。";
    if (!form.host.trim()) return "代理主机不能为空。";
    const port = Number(form.port);
    if (!Number.isFinite(port) || port <= 0 || port > 65535) return "代理端口必须在 1-65535 之间。";
    return null;
  };

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    resetMessages();
    const validationError = validate(createForm);
    if (validationError) {
      setErrorMessage(validationError);
      return;
    }
    setSubmitting(true);
    try {
      const res = await fetch("/api/proxies", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: createForm.name.trim(),
          group: createForm.group.trim(),
          type: createForm.type,
          host: createForm.host.trim(),
          port: Number(createForm.port),
          username: createForm.username.trim(),
          password: createForm.password,
        }),
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "新增代理失败。")
        return;
      }
      setCreateForm(emptyForm);
      setMessage(payload?.message || "代理已添加。")
      await reload();
    } finally {
      setSubmitting(false);
    }
  };

  const startEdit = (proxy: UpstreamProxy) => {
    resetMessages();
    setEditingProxyId(proxy.id);
    setEditForm({
      name: proxy.name,
      group: proxy.group ?? "",
      type: proxy.type,
      host: proxy.host,
      port: String(proxy.port),
      username: proxy.username ?? "",
      password: "",
    });
  };

  const cancelEdit = () => {
    setEditingProxyId(null);
    setEditForm(emptyForm);
  };

  const toggleStatus = async (proxy: UpstreamProxy) => {
    resetMessages();
    setBusyProxyId(proxy.id);
    try {
      const nextStatus = proxy.status === "Disabled" ? "Enabled" : "Disabled";
      const res = await fetch(`/api/proxies/${proxy.id}/status`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status: nextStatus }),
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "更新代理状态失败。")
        return;
      }
      setMessage(payload?.message || (nextStatus === "Disabled" ? "代理已禁用。" : "代理已启用。"))
      await reload();
    } finally {
      setBusyProxyId(null);
    }
  };

  const saveEdit = async (id: number) => {
    resetMessages();
    const validationError = validate(editForm);
    if (validationError) {
      setErrorMessage(validationError);
      return;
    }
    setBusyProxyId(id);
    try {
      const res = await fetch(`/api/proxies/${id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: editForm.name.trim(),
          group: editForm.group.trim(),
          type: editForm.type,
          host: editForm.host.trim(),
          port: Number(editForm.port),
          username: editForm.username.trim(),
          password: editForm.password,
        }),
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "更新代理失败。")
        return;
      }
      setMessage(payload?.message || "代理已更新。")
      cancelEdit();
      await reload();
    } finally {
      setBusyProxyId(null);
    }
  };

  const deleteProxy = async (proxy: UpstreamProxy) => {
    resetMessages();
    if (!window.confirm(`确定要删除代理「${proxy.name}」吗？绑定它的上游 key 会自动解绑。`)) return;
    setBusyProxyId(proxy.id);
    try {
      const res = await fetch(`/api/proxies/${proxy.id}`, { method: "DELETE" });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "删除代理失败。")
        return;
      }
      setMessage(payload?.message || "代理已删除。")
      if (editingProxyId === proxy.id) cancelEdit();
      await reload();
    } finally {
      setBusyProxyId(null);
    }
  };

  const testProxy = async (proxy: UpstreamProxy) => {
    resetMessages();
    setBusyProxyId(proxy.id);
    try {
      const res = await fetch("/api/proxies/test", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ proxyId: proxy.id }),
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "测试代理失败。")
        return;
      }
      setTestResult(payload);
      setMessage(payload?.message || "已完成代理测试。")
      await reload();
    } finally {
      setBusyProxyId(null);
    }
  };

  return (
    <div className="space-y-6">
      <section className="rounded-[30px] border border-slate-200/70 bg-white/90 p-8 shadow-sm">
        <div className="max-w-3xl">
          <div className="text-xs uppercase tracking-[0.24em] text-slate-400">代理池</div>
          <h1 className="mt-3 text-3xl font-semibold tracking-tight text-slate-900">管理上游代理池</h1>
          <p className="mt-3 text-sm leading-7 text-slate-500">这里可以保存 HTTP / HTTPS / SOCKS5 / SOCKS5H 代理，并把它们绑定到具体的 NVIDIA 上游 key。未绑定代理的 key 会继续走系统设置中的全局上游代理。</p>
        </div>
      </section>

      {(message || errorMessage) ? (
        <div className={`rounded-2xl border px-5 py-4 text-sm ${errorMessage ? "border-rose-200 bg-rose-50 text-rose-700" : "border-emerald-200 bg-emerald-50 text-emerald-700"}`}>
          {errorMessage || message}
        </div>
      ) : null}

      {testResult ? (
        <div className={`rounded-2xl border px-5 py-4 text-sm ${testResult.success ? "border-emerald-200 bg-emerald-50 text-emerald-700" : "border-amber-200 bg-amber-50 text-amber-800"}`}>
          <div className="font-medium">最近一次手动测试</div>
          <div className="mt-2">{testResult.message || "已完成测试。"}</div>
          <div className="mt-2 text-xs opacity-80">
            {testResult.target ? <span>目标：{testResult.target}</span> : null}
            {typeof testResult.status_code === "number" ? <span className="ml-4">HTTP {testResult.status_code}</span> : null}
            {typeof testResult.response_time === "number" ? <span className="ml-4">耗时 {testResult.response_time} ms</span> : null}
          </div>
        </div>
      ) : null}

      <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
        <CardHeader>
          <CardTitle>新增代理</CardTitle>
          <CardDescription>新增一个可复用的上游代理节点，供全局设置之外的单 key 定向出站使用。</CardDescription>
        </CardHeader>
        <CardContent>
          <form className="grid gap-4 md:grid-cols-2 xl:grid-cols-3" onSubmit={handleCreate}>
            <Input value={createForm.name} onChange={(e) => setCreateForm({ ...createForm, name: e.target.value })} placeholder="代理名称" />
            <Input value={createForm.group} onChange={(e) => setCreateForm({ ...createForm, group: e.target.value })} placeholder="分组（可选，例如：新加坡）" />
            <Select value={createForm.type} onChange={(e) => setCreateForm({ ...createForm, type: e.target.value })}>
              <option value="http">http</option>
              <option value="https">https</option>
              <option value="socks5">socks5</option>
              <option value="socks5h">socks5h</option>
            </Select>
            <Input value={createForm.host} onChange={(e) => setCreateForm({ ...createForm, host: e.target.value })} placeholder="主机 / IP" />
            <Input value={createForm.port} onChange={(e) => setCreateForm({ ...createForm, port: e.target.value })} placeholder="端口" />
            <Input value={createForm.username} onChange={(e) => setCreateForm({ ...createForm, username: e.target.value })} placeholder="用户名（可选）" />
            <Input type="password" value={createForm.password} onChange={(e) => setCreateForm({ ...createForm, password: e.target.value })} placeholder="密码（可选）" />
            <div className="md:col-span-2 xl:col-span-3">
              <Button type="submit" disabled={submitting}>{submitting ? "保存中..." : "添加代理"}</Button>
            </div>
          </form>
        </CardContent>
      </Card>

      <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
        <CardHeader>
          <CardTitle>代理列表</CardTitle>
          <CardDescription>你可以测试、编辑、禁用或删除代理；删除时会自动解绑引用该代理的上游 key。</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
            <div className="text-sm text-slate-500">默认按代理状态、最近测试成功率、测速耗时和最近测试时间排序。</div>
            <div className="w-full md:w-72">
              <Select value={groupFilter} onChange={(e) => setGroupFilter(e.target.value)}>
                <option value="">全部分组</option>
                {proxyGroups.map((group) => (
                  <option key={group} value={group}>{group}</option>
                ))}
              </Select>
            </div>
          </div>
          {error ? <div className="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">读取代理列表失败，请稍后重试。</div> : null}
          {proxies.length === 0 ? (
            <div className="rounded-xl border border-dashed border-slate-200 px-4 py-10 text-center text-sm text-slate-500">当前还没有保存任何代理。</div>
          ) : proxies.map((proxy) => {
            const isEditing = editingProxyId === proxy.id;
            const isBusy = busyProxyId === proxy.id;
            return (
              <div key={proxy.id} className="rounded-2xl border border-slate-200 bg-slate-50/80 p-4">
                <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
                  <div className="space-y-4">
                    <div>
                      <div className="flex flex-wrap items-center gap-3">
                        <div className="text-lg font-semibold text-slate-900">{proxy.name}</div>
                        <Badge variant="outline">{proxy.type}</Badge>
                        <Badge variant={proxy.status === "Disabled" ? "outline" : "default"}>{proxy.status === "Disabled" ? "已禁用" : "已启用"}</Badge>
                        {proxy.boundKeyCount > 0 ? <Badge variant="default">绑定 {proxy.boundKeyCount} 个 key</Badge> : <Badge variant="outline">未绑定 key</Badge>}
                        <span className="rounded-full border border-slate-200 bg-white px-3 py-1 text-xs text-slate-500">#{String(proxy.id).padStart(4, "0")}</span>
                      </div>
                      <div className="mt-3 grid gap-2 text-sm text-slate-500 md:grid-cols-2 xl:grid-cols-4">
                        <div>分组：{proxy.group?.trim() ? proxy.group : "未分组"}</div>
                        <div>地址：{proxy.host}:{proxy.port}</div>
                        <div>认证：{proxy.hasPassword ? "已设置" : "无密码"}</div>
                        <div>配置更新时间：{formatDate(proxy.updatedAt)}</div>
                      </div>
                    </div>

                    {proxy.lastTest ? (
                      <div className={`rounded-2xl border px-4 py-3 text-sm ${proxy.lastTest.success ? "border-emerald-200 bg-emerald-50/70 text-emerald-800" : "border-amber-200 bg-amber-50/70 text-amber-800"}`}>
                        <div className="flex flex-wrap items-center gap-3">
                          <span className="font-medium">最近一次测试</span>
                          <span>{proxy.lastTest.summary}</span>
                          <span className="text-xs opacity-80">{formatDate(proxy.lastTest.testedAt)}</span>
                        </div>
                        <div className="mt-2 text-xs leading-6 opacity-90">
                          {typeof proxy.lastTest.responseTime === "number" ? <span>耗时 {proxy.lastTest.responseTime} ms</span> : null}
                          {proxy.lastTest.target ? <span className="ml-4 break-all">目标 {proxy.lastTest.target}</span> : null}
                        </div>
                        {proxy.lastTest.message ? <div className="mt-2 text-xs opacity-90">{proxy.lastTest.message}</div> : null}
                      </div>
                    ) : (
                      <div className="rounded-2xl border border-dashed border-slate-200 bg-white/70 px-4 py-3 text-sm text-slate-500">还没有测试记录。</div>
                    )}

                    {(proxy.testHistory?.length ?? 0) > 0 ? (
                      <div className="rounded-2xl border border-slate-200 bg-white/70 px-4 py-3">
                        <div className="text-xs uppercase tracking-[0.2em] text-slate-400">测速历史</div>
                        <div className="mt-3 space-y-2 text-sm text-slate-700">
                          {proxy.testHistory!.slice(0, 5).map((item, index) => (
                            <div key={`${proxy.id}-${item.testedAt}-${index}`} className="flex flex-col gap-1 rounded-xl border border-slate-200 bg-slate-50 px-3 py-2 md:flex-row md:items-center md:justify-between">
                              <div className="flex flex-wrap items-center gap-3">
                                <span className={`inline-flex rounded-full px-2.5 py-1 text-xs ${item.success ? "bg-emerald-100 text-emerald-700" : "bg-amber-100 text-amber-700"}`}>{item.summary}</span>
                                <span className="text-xs text-slate-500">{formatDate(item.testedAt)}</span>
                              </div>
                              <div className="text-xs text-slate-500">{typeof item.responseTime === "number" ? `${item.responseTime} ms` : "-"}</div>
                            </div>
                          ))}
                        </div>
                      </div>
                    ) : null}
                  </div>

                  <div className="flex flex-wrap gap-2 xl:max-w-sm xl:justify-end">
                    <Button variant="outline" size="sm" onClick={() => (isEditing ? cancelEdit() : startEdit(proxy))} disabled={isBusy}>{isEditing ? "收起编辑" : "编辑"}</Button>
                    <Button variant="outline" size="sm" onClick={() => toggleStatus(proxy)} disabled={isBusy}>{proxy.status === "Disabled" ? "启用" : "禁用"}</Button>
                    <Button variant="outline" size="sm" onClick={() => testProxy(proxy)} disabled={isBusy}>测试</Button>
                    <Button variant="destructive" size="sm" onClick={() => deleteProxy(proxy)} disabled={isBusy}>删除</Button>
                  </div>
                </div>
                {isEditing ? (
                  <div className="mt-4 grid gap-3 rounded-2xl border border-slate-200 bg-white p-4 md:grid-cols-3">
                    <Input value={editForm.name} onChange={(e) => setEditForm({ ...editForm, name: e.target.value })} placeholder="代理名称" />
                    <Input value={editForm.group} onChange={(e) => setEditForm({ ...editForm, group: e.target.value })} placeholder="分组（可选）" />
                    <Select value={editForm.type} onChange={(e) => setEditForm({ ...editForm, type: e.target.value })}>
                      <option value="http">http</option>
                      <option value="https">https</option>
                      <option value="socks5">socks5</option>
                      <option value="socks5h">socks5h</option>
                    </Select>
                    <Input value={editForm.host} onChange={(e) => setEditForm({ ...editForm, host: e.target.value })} placeholder="主机 / IP" />
                    <Input value={editForm.port} onChange={(e) => setEditForm({ ...editForm, port: e.target.value })} placeholder="端口" />
                    <Input value={editForm.username} onChange={(e) => setEditForm({ ...editForm, username: e.target.value })} placeholder="用户名（可选）" />
                    <Input type="password" value={editForm.password} onChange={(e) => setEditForm({ ...editForm, password: e.target.value })} placeholder="留空表示保留当前密码" />
                    <div className="md:col-span-3 flex gap-3">
                      <Button size="sm" onClick={() => saveEdit(proxy.id)} disabled={isBusy}>保存</Button>
                      <Button variant="outline" size="sm" onClick={cancelEdit}>取消</Button>
                    </div>
                  </div>
                ) : null}
              </div>
            );
          })}
        </CardContent>
      </Card>
    </div>
  );
}

function formatDate(value: string) {
  return new Date(value).toLocaleString();
}
