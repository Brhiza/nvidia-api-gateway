"use client";

import { useEffect, useMemo, useState } from "react";
import useSWR from "swr";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";

const fetcher = async (url: string) => {
  const res = await fetch(url);
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error(data?.error || "request_failed");
  }
  return data;
};

interface APIKey {
  id: number;
  name: string;
  weight: number;
  status: string;
  probeOnly: boolean;
  proxyId?: number;
  proxyName?: string;
  proxyGroup?: string;
  createdAt: string;
  updatedAt: string;
}

interface SchedulerStats {
  active: number;
  cooling: number;
  dead: number;
}

interface DashboardResponse {
  keys: APIKey[];
  stats: SchedulerStats;
}

interface UpstreamProxy {
  id: number;
  name: string;
  group?: string;
  type: string;
  status: string;
}

interface ProxyListResponse {
  proxies: UpstreamProxy[];
}

interface KeyFormState {
  name: string;
  key: string;
  weight: string;
  probeOnly: boolean;
  proxyId: string;
}

interface ProbeInfo {
  endpoint: string;
  method: string;
  httpStatus: number;
  durationMs: number;
  detail: string;
}

const emptyForm: KeyFormState = { name: "", key: "", weight: "1.0", probeOnly: false, proxyId: "" };

export default function APIKeysPage() {
  const { data, error, mutate } = useSWR<DashboardResponse>("/api/keys", fetcher, {
    refreshInterval: 5000,
  });
  const { data: proxyData, mutate: mutateProxies } = useSWR<ProxyListResponse>("/api/proxies", fetcher, {
    refreshInterval: 5000,
  });

  const stats = data?.stats ?? { active: 0, cooling: 0, dead: 0 };
  const sortedKeys = useMemo(() => [...(data?.keys ?? [])].sort((a, b) => a.id - b.id), [data?.keys]);
  const proxies = useMemo(() => [...(proxyData?.proxies ?? [])].sort((a, b) => a.id - b.id), [proxyData?.proxies]);
  const bindableProxies = useMemo(() => proxies.filter((proxy) => proxy.status !== "Disabled"), [proxies]);
  const proxyGroups = useMemo(() => {
    const seen = new Set<string>();
    for (const proxy of bindableProxies) {
      if (!proxy.group?.trim()) continue;
      seen.add(proxy.group.trim());
    }
    return Array.from(seen).sort((a, b) => a.localeCompare(b));
  }, [bindableProxies]);

  const [createForm, setCreateForm] = useState<KeyFormState>(emptyForm);
  const [editingKeyId, setEditingKeyId] = useState<number | null>(null);
  const [editForm, setEditForm] = useState<KeyFormState>(emptyForm);
  const [bulkPayload, setBulkPayload] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isReloading, setIsReloading] = useState(false);
  const [busyKeyId, setBusyKeyId] = useState<number | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [probeInfo, setProbeInfo] = useState<ProbeInfo | null>(null);
  const [importSummary, setImportSummary] = useState<string[]>([]);
  const [selectedKeyIds, setSelectedKeyIds] = useState<number[]>([]);
  const [bulkProxyId, setBulkProxyId] = useState<string>("");
  const [bulkProxyGroup, setBulkProxyGroup] = useState<string>("");
  const [bulkBinding, setBulkBinding] = useState(false);

  useEffect(() => {
    if (!message && !errorMessage) return;
    const timer = window.setTimeout(() => {
      setMessage(null);
      setErrorMessage(null);
    }, 4000);
    return () => window.clearTimeout(timer);
  }, [message, errorMessage]);

  const resetMessages = () => {
    setMessage(null);
    setErrorMessage(null);
    setProbeInfo(null);
  };

  const reloadData = async () => {
    await Promise.all([mutate(), mutateProxies()]);
  };

  const activeSelectedKeyIds = useMemo(
    () => selectedKeyIds.filter((id) => sortedKeys.some((item) => item.id === id)),
    [selectedKeyIds, sortedKeys],
  );

  const allVisibleSelected = sortedKeys.length > 0 && sortedKeys.every((item) => activeSelectedKeyIds.includes(item.id));

  const toggleKeySelection = (id: number, checked: boolean) => {
    setSelectedKeyIds((current) => {
      if (checked) {
        return current.includes(id) ? current : [...current, id].sort((a, b) => a - b);
      }
      return current.filter((item) => item !== id);
    });
  };

  const toggleSelectAllVisible = () => {
    if (allVisibleSelected) {
      setSelectedKeyIds([]);
      return;
    }
    setSelectedKeyIds(sortedKeys.map((item) => item.id));
  };

  const clearSelectedKeys = () => {
    setSelectedKeyIds([]);
  };

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    resetMessages();
    if (!createForm.name.trim() || !createForm.key.trim()) {
      setErrorMessage("名称和 key 不能为空。")
      return;
    }
    if (Number(createForm.weight) <= 0) {
      setErrorMessage("权重必须大于 0。")
      return;
    }
    setIsSubmitting(true);
    try {
      const res = await fetch("/api/keys", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: createForm.name.trim(),
          key: createForm.key.trim(),
          weight: Number(createForm.weight),
          probeOnly: createForm.probeOnly,
          proxyId: createForm.proxyId ? Number(createForm.proxyId) : 0,
        }),
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "新增失败。")
        return;
      }
      setCreateForm(emptyForm);
      setMessage(payload?.message || "已添加新的上游 key。")
      await reloadData();
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleBulkImport = async () => {
    resetMessages();
    setImportSummary([]);
    if (!bulkPayload.trim()) {
      setErrorMessage("请先粘贴要导入的 key。")
      return;
    }
    setIsSubmitting(true);
    try {
      const res = await fetch("/api/keys/import", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ payload: bulkPayload, defaultWeight: 1 }),
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "批量导入失败。")
        return;
      }
      const result = payload?.result;
      setMessage(payload?.message || "批量导入完成。")
      setImportSummary([
        `新增 ${result?.addedCount ?? 0} 个`,
        `跳过 ${result?.skippedCount ?? 0} 个`,
        ...((result?.skipped as string[] | undefined) ?? []).slice(0, 8),
      ]);
      setBulkPayload("");
      await reloadData();
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleReload = async () => {
    resetMessages();
    setIsReloading(true);
    try {
      const res = await fetch("/api/reload", { method: "POST" });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "热重载失败。")
        return;
      }
      setMessage(payload?.message || "调度器已重新加载。")
      await reloadData();
    } finally {
      setIsReloading(false);
    }
  };

  const handleBulkProxyBinding = async (mode: "proxy" | "group" | "clear") => {
    resetMessages();
    if (activeSelectedKeyIds.length === 0) {
      setErrorMessage("请先选择至少一个上游 key。")
      return;
    }
    if (mode === "proxy" && !bulkProxyId) {
      setErrorMessage("请先选择一个代理。")
      return;
    }
    if (mode === "group" && !bulkProxyGroup) {
      setErrorMessage("请先选择一个代理分组。")
      return;
    }
    setBulkBinding(true);
    try {
      const body: Record<string, unknown> = { keyIds: activeSelectedKeyIds };
      if (mode === "proxy") {
        body.proxyId = Number(bulkProxyId);
      } else if (mode === "group") {
        body.proxyGroup = bulkProxyGroup;
      } else {
        body.proxyId = 0;
      }
      const res = await fetch("/api/keys/proxy", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "批量绑定代理失败。")
        return;
      }
      const updatedCount = payload?.result?.updatedCount ?? activeSelectedKeyIds.length;
      setMessage(payload?.message || `已批量处理 ${updatedCount} 个 key。`)
      clearSelectedKeys();
      if (mode === "clear") {
        setBulkProxyId("");
        setBulkProxyGroup("");
      }
      await reloadData();
    } finally {
      setBulkBinding(false);
    }
  };

  const startEdit = (key: APIKey) => {
    resetMessages();
    setEditingKeyId(key.id);
    setEditForm({ name: key.name, key: "", weight: key.weight.toString(), probeOnly: key.probeOnly, proxyId: key.proxyId ? String(key.proxyId) : "" });
  };

  const cancelEdit = () => {
    setEditingKeyId(null);
    setEditForm(emptyForm);
  };

  const submitEdit = async (id: number) => {
    resetMessages();
    if (!editForm.name.trim()) {
      setErrorMessage("名称不能为空。")
      return;
    }
    if (Number(editForm.weight) <= 0) {
      setErrorMessage("权重必须大于 0。")
      return;
    }
    setBusyKeyId(id);
    try {
      const res = await fetch(`/api/keys/${id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: editForm.name.trim(),
          key: editForm.key.trim(),
          weight: Number(editForm.weight),
          probeOnly: editForm.probeOnly,
          proxyId: editForm.proxyId ? Number(editForm.proxyId) : 0,
        }),
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "保存失败。")
        return;
      }
      setMessage(payload?.message || "已保存修改。")
      cancelEdit();
      await reloadData();
    } finally {
      setBusyKeyId(null);
    }
  };

  const updateStatus = async (id: number, status: "Active" | "Disabled") => {
    resetMessages();
    setBusyKeyId(id);
    try {
      const res = await fetch(`/api/keys/${id}/status`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status }),
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "更新状态失败。")
        return;
      }
      setMessage(payload?.message || "状态已更新。")
      await reloadData();
    } finally {
      setBusyKeyId(null);
    }
  };

  const toggleProbeOnly = async (key: APIKey) => {
    resetMessages();
    setBusyKeyId(key.id);
    try {
      const res = await fetch(`/api/keys/${key.id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ probeOnly: !key.probeOnly }),
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "切换健康专用标记失败。")
        return;
      }
      setMessage(payload?.message || (!key.probeOnly ? "已设为健康检查专用 key。" : "已取消健康检查专用标记。"))
      await reloadData();
    } finally {
      setBusyKeyId(null);
    }
  };

  const probeKey = async (id: number) => {
    resetMessages();
    setBusyKeyId(id);
    try {
      const res = await fetch(`/api/keys/${id}/probe`, { method: "POST" });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "探测失败。")
        return;
      }
      setMessage(payload?.message || "已完成一次探测。")
      await reloadData();
    } finally {
      setBusyKeyId(null);
    }
  };

  const deleteKey = async (id: number) => {
    resetMessages();
    if (!window.confirm(`确定要删除上游 key #${id.toString().padStart(4, "0")} 吗？`)) return;
    setBusyKeyId(id);
    try {
      const res = await fetch(`/api/keys/${id}`, { method: "DELETE" });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setErrorMessage(payload?.error || "删除失败。")
        return;
      }
      setMessage(payload?.message || "上游 key 已删除。")
      if (editingKeyId === id) cancelEdit();
      await reloadData();
    } finally {
      setBusyKeyId(null);
    }
  };

  return (
    <div className="space-y-6">
      <section className="rounded-[30px] border border-slate-200/70 bg-white/90 p-8 shadow-sm">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <div className="text-xs uppercase tracking-[0.24em] text-slate-400">上游 key</div>
            <h1 className="mt-3 text-3xl font-semibold tracking-tight text-slate-900">管理 NVIDIA 上游 key</h1>
            <p className="mt-3 max-w-3xl text-sm leading-7 text-slate-500">
              {"可以新增单个 key、批量导入、调整权重、探测状态和热重载调度器。若某个 key 只想给健康检查使用，可以开启“健康专用”，这样它不会进入正常业务调度池。"}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <div className="rounded-full border border-slate-200 bg-slate-50 px-4 py-2 text-sm text-slate-600">可用 {stats.active} · 冷却 {stats.cooling} · 隔离/禁用 {stats.dead}</div>
            <Button onClick={handleReload} disabled={isReloading}>{isReloading ? "重载中..." : "热重载调度器"}</Button>
          </div>
        </div>
      </section>

      {(message || errorMessage) && (
        <div className={`rounded-2xl border px-5 py-4 text-sm ${errorMessage ? "border-rose-200 bg-rose-50 text-rose-700" : "border-emerald-200 bg-emerald-50 text-emerald-700"}`}>
          <div>{errorMessage || message}</div>
          {probeInfo ? (
            <div className="mt-2 text-xs leading-6 text-slate-600">
              本次探测调用的是 NVIDIA 官方 /models 接口，用来判断该上游 Key 是否可用 / 冷却 / 已失效。
            </div>
          ) : null}
        </div>
      )}

      {probeInfo ? (
        <div className="rounded-2xl border border-slate-200 bg-white/90 px-5 py-4 text-sm text-slate-700 shadow-sm">
          <div className="text-xs uppercase tracking-[0.2em] text-slate-400">探测详情</div>
          <div className="mt-3 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <div><span className="text-slate-400">方法：</span><span className="font-mono">{probeInfo.method}</span></div>
            <div><span className="text-slate-400">{"HTTP 状态："}</span><span className="font-mono">{probeInfo.httpStatus}</span></div>
            <div><span className="text-slate-400">耗时：</span><span className="font-mono">{probeInfo.durationMs} ms</span></div>
            <div className="md:col-span-2 xl:col-span-4 break-all"><span className="text-slate-400">接口：</span><span className="font-mono text-xs">{probeInfo.endpoint}</span></div>
          </div>
          <div className="mt-3 text-slate-600">{probeInfo.detail}</div>
        </div>
      ) : null}

      <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
        <CardHeader>
          <CardTitle>{"批量绑定代理"}</CardTitle>
          <CardDescription>{"先勾选下面的上游 key，再统一绑定到某个代理，或一键清空代理绑定回到全局代理。"}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <div className="text-sm text-slate-600">{"已选择 "}<span className="font-semibold text-slate-900">{activeSelectedKeyIds.length}</span>{" 个 key"}</div>
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" size="sm" onClick={toggleSelectAllVisible}>{allVisibleSelected ? "取消全选当前列表" : "全选当前列表"}</Button>
              <Button variant="outline" size="sm" onClick={clearSelectedKeys}>{"清空选择"}</Button>
            </div>
          </div>
          <div className="grid gap-3 md:grid-cols-[1fr_auto_auto]">
            <Select value={bulkProxyId} onChange={(e) => setBulkProxyId(e.target.value)}>
              <option value="">{"选择要绑定的代理"}</option>
              {bindableProxies.map((proxy) => (
                <option key={proxy.id} value={String(proxy.id)}>{proxy.name} {"·"} {proxy.type}{proxy.group ? ` · ${proxy.group}` : ""}</option>
              ))}
            </Select>
            <Button onClick={() => handleBulkProxyBinding("proxy")} disabled={bulkBinding || activeSelectedKeyIds.length === 0 || !bulkProxyId}>{bulkBinding ? "处理中..." : "按代理批量绑定"}</Button>
            <Button variant="outline" onClick={() => handleBulkProxyBinding("clear")} disabled={bulkBinding || activeSelectedKeyIds.length === 0}>{"批量清空代理"}</Button>
          </div>
          <div className="grid gap-3 md:grid-cols-[1fr_auto]">
            <Select value={bulkProxyGroup} onChange={(e) => setBulkProxyGroup(e.target.value)}>
              <option value="">{"选择要轮询分配的分组"}</option>
              {proxyGroups.map((group) => (
                <option key={group} value={group}>{group}</option>
              ))}
            </Select>
            <Button variant="outline" onClick={() => handleBulkProxyBinding("group")} disabled={bulkBinding || activeSelectedKeyIds.length === 0 || !bulkProxyGroup}>{bulkBinding ? "处理中..." : "按分组轮询绑定"}</Button>
          </div>
          <div className="text-xs leading-6 text-slate-500">{"没有绑定代理的 key，会继续跟随系统设置里的全局上游代理；如果全局代理也为空，就按环境变量或直连逻辑处理。"}</div>
        </CardContent>
      </Card>

      <section className="grid gap-6 xl:grid-cols-[0.9fr_1.1fr]">
        <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
          <CardHeader>
            <CardTitle>新增单个 key</CardTitle>
            <CardDescription>适合手动录入少量 key。真实值不会作为示例明文显示。</CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleCreate} className="space-y-4">
              <Input value={createForm.name} onChange={(e) => setCreateForm({ ...createForm, name: e.target.value })} placeholder="例如：NVIDIA-01" />
              <Input type="password" value={createForm.key} onChange={(e) => setCreateForm({ ...createForm, key: e.target.value })} placeholder="直接粘贴完整 nvapi-..." />
              <Input type="number" min="0.1" step="0.1" value={createForm.weight} onChange={(e) => setCreateForm({ ...createForm, weight: e.target.value })} placeholder="1.0" />
              <Select value={createForm.proxyId} onChange={(e) => setCreateForm({ ...createForm, proxyId: e.target.value })}>
                <option value="">跟随全局代理 / 当前全局直连</option>
                {bindableProxies.map((proxy) => (
                  <option key={proxy.id} value={String(proxy.id)}>{proxy.name} · {proxy.type}</option>
                ))}
              </Select>
              <label className="flex items-center justify-between rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-700">
                <div>
                  <div className="font-medium text-slate-900">{"健康专用"}</div>
                  <div className="text-xs text-slate-500">{"开启后，这个 key 只用于健康检查，不会进入正常业务调度池。"}</div>
                </div>
                <Switch checked={createForm.probeOnly} onCheckedChange={(value) => setCreateForm({ ...createForm, probeOnly: value })} />
              </label>
              <Button type="submit" disabled={isSubmitting}>{isSubmitting ? "提交中..." : "添加 key"}</Button>
            </form>
          </CardContent>
        </Card>

        <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
          <CardHeader>
            <CardTitle>批量导入</CardTitle>
            <CardDescription>支持一行一个 key，也支持 `name,key,weight`、`name|key|weight` 这样的格式。</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <Textarea
              value={bulkPayload}
              onChange={(e) => setBulkPayload(e.target.value)}
              className="h-52 font-mono text-xs"
              placeholder={"NVIDIA-01,nvapi-************,1.0\nNVIDIA-02,nvapi-************,0.8"}
            />
            <div className="flex gap-3">
              <Button onClick={handleBulkImport} disabled={isSubmitting}>{isSubmitting ? "导入中..." : "开始导入"}</Button>
              <Button variant="outline" onClick={() => setBulkPayload("")}>清空</Button>
            </div>
            {importSummary.length > 0 ? (
              <div className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-600">
                {importSummary.map((item) => (
                  <div key={item}>{item}</div>
                ))}
              </div>
            ) : null}
          </CardContent>
        </Card>
      </section>

      <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
        <CardHeader>
          <CardTitle>当前 key 列表</CardTitle>
          <CardDescription>可以编辑名称和权重，也可以直接禁用、探测或删除。</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {error ? <div className="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">读取 key 列表失败，请稍后重试。</div> : null}
          {sortedKeys.length === 0 ? (
            <div className="rounded-xl border border-dashed border-slate-200 px-4 py-10 text-center text-sm text-slate-500">当前还没有上游 key。</div>
          ) : (
            sortedKeys.map((key) => {
              const isEditing = editingKeyId === key.id;
              const isBusy = busyKeyId === key.id;
              return (
                <div key={key.id} className="rounded-2xl border border-slate-200 bg-slate-50/80 p-4">
                  <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
                    <div>
                      <div className="flex flex-wrap items-center gap-3">
                        <label className="inline-flex items-center gap-2 rounded-full border border-slate-200 bg-white px-3 py-1 text-xs text-slate-600">
                          <input
                            type="checkbox"
                            className="h-4 w-4 rounded border-slate-300"
                            checked={activeSelectedKeyIds.includes(key.id)}
                            onChange={(e) => toggleKeySelection(key.id, e.target.checked)}
                          />
                          {"选择"}
                        </label>
                        <div className="text-lg font-semibold text-slate-900">{key.name}</div>
                        <Badge variant={key.status === "Active" ? "default" : "outline"}>{renderStatus(key.status)}</Badge>
                        {key.probeOnly ? <Badge variant="outline">{"健康专用"}</Badge> : <Badge variant="outline">{"业务共享"}</Badge>}
                        <span className="rounded-full border border-slate-200 bg-white px-3 py-1 text-xs text-slate-500">#{String(key.id).padStart(4, "0")}</span>
                      </div>
                      <div className="mt-3 grid gap-2 text-sm text-slate-500 md:grid-cols-2 xl:grid-cols-4">
                        <div>权重：{key.weight.toFixed(1)}</div>
                        <div>{"用途："}{key.probeOnly ? "健康检查专用" : "参与业务调度"}</div>
                        <div>{"代理："}{key.proxyName ? key.proxyName : "跟随全局代理 / 全局直连"}</div>
                        <div>创建时间：{formatDate(key.createdAt)}</div>
                        <div className="md:col-span-2">更新时间：{formatDate(key.updatedAt)}</div>
                      </div>
                    </div>
                    <div className="flex flex-wrap gap-2 xl:max-w-sm xl:justify-end">
                      <Button variant="outline" size="sm" onClick={() => (isEditing ? cancelEdit() : startEdit(key))} disabled={isBusy}>{isEditing ? "收起编辑" : "编辑"}</Button>
                      <Button variant="outline" size="sm" onClick={() => updateStatus(key.id, key.status === "Disabled" ? "Active" : "Disabled")} disabled={isBusy}>{key.status === "Disabled" ? "启用" : "禁用"}</Button>
                      <Button variant="outline" size="sm" onClick={() => toggleProbeOnly(key)} disabled={isBusy}>{key.probeOnly ? "取消健康专用" : "设为健康专用"}</Button>
                      <Button variant="outline" size="sm" onClick={() => probeKey(key.id)} disabled={isBusy}>探测</Button>
                      <Button variant="destructive" size="sm" onClick={() => deleteKey(key.id)} disabled={isBusy}>删除</Button>
                    </div>
                  </div>

                  {isEditing ? (
                    <div className="mt-4 grid gap-3 rounded-2xl border border-slate-200 bg-white p-4 md:grid-cols-6">
                      <Input value={editForm.name} onChange={(e) => setEditForm({ ...editForm, name: e.target.value })} placeholder="名称" />
                      <Input type="password" value={editForm.key} onChange={(e) => setEditForm({ ...editForm, key: e.target.value })} placeholder="留空则不修改 key" />
                      <Input type="number" min="0.1" step="0.1" value={editForm.weight} onChange={(e) => setEditForm({ ...editForm, weight: e.target.value })} placeholder="权重" />
                      <Select value={editForm.proxyId} onChange={(e) => setEditForm({ ...editForm, proxyId: e.target.value })}>
                        <option value="">跟随全局代理 / 当前全局直连</option>
                        {bindableProxies.map((proxy) => (
                          <option key={proxy.id} value={String(proxy.id)}>{proxy.name} · {proxy.type}</option>
                        ))}
                      </Select>
                      <label className="flex items-center justify-between rounded-2xl border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-700">
                        <span>{"健康专用"}</span>
                        <Switch checked={editForm.probeOnly} onCheckedChange={(value) => setEditForm({ ...editForm, probeOnly: value })} />
                      </label>
                      <div className="flex gap-3">
                        <Button size="sm" onClick={() => submitEdit(key.id)} disabled={isBusy}>保存</Button>
                        <Button variant="outline" size="sm" onClick={cancelEdit}>取消</Button>
                      </div>
                    </div>
                  ) : null}
                </div>
              );
            })
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function renderStatus(status: string) {
  switch (status) {
    case "Active":
      return "可用";
    case "Cooling":
      return "冷却中";
    case "Dead":
      return "已隔离";
    case "Disabled":
      return "已禁用";
    default:
      return status;
  }
}

function formatDate(value: string) {
  return new Date(value).toLocaleString();
}

