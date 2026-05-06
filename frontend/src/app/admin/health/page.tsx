"use client";

import { useMemo, useState } from "react";
import useSWR from "swr";

import { HorizontalBarChart, MetricCard, SparkAreaChart, StatusBadge } from "@/components/dashboard/visuals";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Select } from "@/components/ui/select";

const fetcher = async (url: string) => {
  const res = await fetch(url, { cache: "no-store" });
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error(data?.error || "request_failed");
  }
  return data;
};

type HealthReport = {
  generatedAt: string;
  upstreamBaseURL: string;
  probeKeyName: string;
  probeKeyDedicated: boolean;
  probeTimeoutSecond: number;
  summary: {
    overallStatus: string;
    totalKeys: number;
    activeKeys: number;
    coolingKeys: number;
    deadKeys: number;
    disabledKeys: number;
    healthyChecks: number;
    unhealthyChecks: number;
    avgLatencyMs: number;
  };
  schedulerStats: {
    active: number;
    cooling: number;
    dead: number;
  };
  keys: Array<{
    id: number;
    name: string;
    weight: number;
    status: string;
    updatedAt: string;
  }>;
  checks: Array<{
    id: string;
    title: string;
    method: string;
    endpoint: string;
    success: boolean;
    httpStatus: number;
    durationMs: number;
    statusLabel: string;
    detail: string;
    meta?: Record<string, unknown>;
  }>;
  history: Array<{
    generatedAt: string;
    label: string;
    avgLatencyMs: number;
    healthyChecks: number;
    unhealthyChecks: number;
    overallStatus: string;
  }>;
  recommendations: string[];
  modelCatalog: Array<{
    id: string;
    supportsChatCandidate: boolean;
    supportsEmbeddingsCandidate: boolean;
  }>;
  activeRun?: ModelRun | null;
  fullSweep?: ModelRun | null;
};

type ModelRun = {
  generatedAt: string;
  scope: string;
  protocol: string;
  selectedModelId: string;
  summary: {
    total: number;
    healthy: number;
    failed: number;
    avgLatencyMs: number;
  };
  checks: Array<{
    generatedAt: string;
    modelId: string;
    protocol: string;
    method: string;
    endpoint: string;
    success: boolean;
    httpStatus: number;
    durationMs: number;
    statusLabel: string;
    detail: string;
    attemptCount: number;
    meta?: Record<string, unknown>;
  }>;
  latencyChart: Array<{ label: string; value: number; meta?: string }>;
};

type UpstreamRuntimeSnapshot = {
  generatedAt: string;
  summary: {
    totalKeys: number;
    activeKeys: number;
    coolingKeys: number;
    deadKeys: number;
    disabledKeys: number;
    schedulerStats?: {
      active: number;
      cooling: number;
      dead: number;
    } | null;
  };
  lastEvent?: {
    at: string;
    operation: string;
    operationLabel?: string;
    stage: string;
    stageLabel?: string;
    upstreamKeyName?: string;
    success: boolean;
    httpStatus?: number;
    detail?: string;
  } | null;
  recentEvents: Array<{
    at: string;
    operation: string;
    operationLabel?: string;
    stage: string;
    stageLabel?: string;
    upstreamKeyName?: string;
    success: boolean;
    httpStatus?: number;
    detail?: string;
  }>;
};

export default function HealthPage() {
  const { data, error, mutate, isLoading } = useSWR<HealthReport>("/api/health/report", fetcher);
  const { data: runtimeData } = useSWR<UpstreamRuntimeSnapshot>("/api/upstream/runtime", fetcher, { refreshInterval: 3000 });

  const [running, setRunning] = useState(false);
  const [scope, setScope] = useState<"all" | "single">("all");
  const [protocol, setProtocol] = useState<"auto" | "chat" | "embeddings">("auto");
  const [selectedModelId, setSelectedModelId] = useState("");
  const [runError, setRunError] = useState<string | null>(null);

  const modelCatalog = Array.isArray(data?.modelCatalog) ? data.modelCatalog : [];
  const effectiveSelectedModelId = selectedModelId || modelCatalog[0]?.id || "";

  const keyStatusData = useMemo(() => {
    if (!data) return [];
    return [
      { label: "可用", value: data.summary.activeKeys, meta: "Active" },
      { label: "冷却中", value: data.summary.coolingKeys, meta: "Cooling" },
      { label: "已隔离", value: data.summary.deadKeys, meta: "Dead" },
      { label: "已禁用", value: data.summary.disabledKeys, meta: "Disabled" },
    ];
  }, [data]);

  const baseLatencyData = useMemo(() => {
    if (!data) return [];
    return (data.checks ?? []).map((check) => ({
      label: check.title.replace("NVIDIA 官方 ", ""),
      value: check.durationMs,
      meta: `${check.httpStatus || "-"}`,
    }));
  }, [data]);

  const historyData = useMemo(() => {
    if (!data) return [];
    return (data.history ?? []).map((item) => ({ label: item.label, value: Math.round(item.avgLatencyMs) }));
  }, [data]);

  const activeRunChecks = Array.isArray(data?.activeRun?.checks) ? data.activeRun.checks : [];
  const activeRunSucceededModels = activeRunChecks.filter((check) => check.success);
  const activeRunFailedModels = activeRunChecks.filter((check) => !check.success);
  const fullSweepChart = Array.isArray(data?.fullSweep?.latencyChart) ? data.fullSweep.latencyChart : [];
  const activeRunChart = Array.isArray(data?.activeRun?.latencyChart) ? data.activeRun.latencyChart : [];

  const runHealthCheck = async () => {
    setRunning(true);
    setRunError(null);
    try {
      const res = await fetch("/api/health/report", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          scope,
          modelId: scope === "single" ? effectiveSelectedModelId : "",
          protocol,
        }),
      });
      const payload = await res.json().catch(() => null);
      if (!res.ok) {
        setRunError(payload?.error || "执行健康检查失败。");
        return;
      }
      await mutate(payload as HealthReport, { revalidate: false });
    } finally {
      setRunning(false);
    }
  };

  return (
    <div className="space-y-6 text-slate-900">
      <section className="rounded-[30px] border border-slate-200/70 bg-[radial-gradient(circle_at_top_left,rgba(186,230,253,0.7),transparent_28%),linear-gradient(180deg,rgba(255,255,255,0.96),rgba(248,250,252,0.94))] p-8 shadow-sm">
        <div className="flex flex-col gap-6 xl:flex-row xl:items-end xl:justify-between">
          <div>
            <div className="text-xs uppercase tracking-[0.24em] text-slate-400">健康检查</div>
            <h1 className="mt-3 text-3xl font-semibold tracking-tight text-slate-900 md:text-4xl">查看网关和所有模型是否正常</h1>
            <p className="mt-3 max-w-3xl text-sm leading-7 text-slate-500">
              {"当前页面默认只展示缓存结果，不会在打开时自动探测上游；只有点击“立即检查”后，才会真实访问 NVIDIA 官方 API。现在 chat 探测只等待首个输出块，并按单并发执行，尽量不影响正常用户请求。"}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            {data ? <StatusBadge status={data.summary.overallStatus} className="text-xs" /> : null}
            <Button onClick={runHealthCheck} disabled={running || (scope === "single" && !effectiveSelectedModelId)} size="lg">
              {running ? "检测中..." : "立即检查"}
            </Button>
          </div>
        </div>
        <div className="mt-6 grid gap-4 md:grid-cols-3">
          <label className="space-y-2 block">
            <span className="text-sm text-slate-500">检查范围</span>
            <Select value={scope} onChange={(e) => setScope(e.target.value as "all" | "single") }>
              <option value="all">全部模型</option>
              <option value="single">单个模型</option>
            </Select>
          </label>
          <label className="space-y-2 block">
            <span className="text-sm text-slate-500">协议模式</span>
            <Select value={protocol} onChange={(e) => setProtocol(e.target.value as "auto" | "chat" | "embeddings") }>
              <option value="auto">智能模式</option>
              <option value="chat">Chat</option>
              <option value="embeddings">Embeddings</option>
            </Select>
          </label>
          <label className="space-y-2 block">
            <span className="text-sm text-slate-500">模型</span>
            <Select value={scope === "all" ? "__all__" : effectiveSelectedModelId} onChange={(e) => setSelectedModelId(e.target.value)} disabled={scope === "all"}>
              <option value="__all__">全部模型</option>
              {modelCatalog.map((item, index) => (
                <option key={`${item.id}-${index}`} value={item.id}>{item.id}</option>
              ))}
            </Select>
          </label>
        </div>
        {scope === "single" && modelCatalog.length === 0 ? (
          <div className="mt-4 rounded-2xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
            {"当前还没有缓存模型目录。请先执行一次“立即检查”，拿到最新模型列表后，再做单模型探测。"}
          </div>
        ) : null}
      </section>

      {error ? <div className="rounded-2xl border border-rose-200 bg-rose-50 px-5 py-4 text-sm text-rose-700">读取健康报告失败：{error.message}</div> : null}
      {runError ? <div className="rounded-2xl border border-rose-200 bg-rose-50 px-5 py-4 text-sm text-rose-700">{runError}</div> : null}
      {isLoading && !data ? <div className="rounded-2xl border border-slate-200 bg-white px-5 py-12 text-center text-sm text-slate-500">正在读取健康报告...</div> : null}


      {runtimeData ? (
        <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
          <CardHeader>
            <CardTitle>上游池实时状态</CardTitle>
            <CardDescription>这里显示当前 NVIDIA 官方 Key 池状态，以及最近一次请求最终选中了哪个上游 Key、卡在哪一步。</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-5">
              <MetricCard label="总上游 Key" value={`${runtimeData.summary.totalKeys}`} tone="neutral" />
              <MetricCard label="可用 Key" value={`${runtimeData.summary.activeKeys}`} tone="success" />
              <MetricCard label="冷却中" value={`${runtimeData.summary.coolingKeys}`} tone={runtimeData.summary.coolingKeys > 0 ? "warning" : "neutral"} />
              <MetricCard label="已隔离" value={`${runtimeData.summary.deadKeys}`} tone={runtimeData.summary.deadKeys > 0 ? "warning" : "neutral"} />
              <MetricCard label="调度池" value={`${runtimeData.summary.schedulerStats?.active ?? 0}`} delta={`冷却 ${runtimeData.summary.schedulerStats?.cooling ?? 0} / 隔离 ${runtimeData.summary.schedulerStats?.dead ?? 0}`} tone="accent" />
            </div>

            <div className="rounded-2xl border border-slate-200 bg-slate-50 p-4 text-sm text-slate-700">
              <div className="mb-2 text-xs uppercase tracking-[0.2em] text-slate-400">最近一次上游事件</div>
              {runtimeData.lastEvent ? (
                <div className="space-y-2">
                  <div>{"时间："}{new Date(runtimeData.lastEvent.at).toLocaleString()}</div>
                  <div>
                    {"操作："}<span className="font-medium text-slate-900">{runtimeData.lastEvent.operationLabel || runtimeData.lastEvent.operation}</span>
                    <span className="ml-2 font-mono text-xs text-slate-500">{runtimeData.lastEvent.operation}</span>
                  </div>
                  <div>
                    {"阶段："}<span className="font-medium text-slate-900">{runtimeData.lastEvent.stageLabel || runtimeData.lastEvent.stage}</span>
                    <span className="ml-2 font-mono text-xs text-slate-500">{runtimeData.lastEvent.stage}</span>
                  </div>
                  <div>{"上游 Key："}<span className="font-medium text-slate-900">{runtimeData.lastEvent.upstreamKeyName || "（还未选中任何 NVIDIA 官方 Key）"}</span></div>
                  <div>{"结果："}{runtimeData.lastEvent.success ? "成功" : "失败"}{runtimeData.lastEvent.httpStatus ? ` / HTTP ${runtimeData.lastEvent.httpStatus}` : ""}</div>
                  {runtimeData.lastEvent.detail ? <div className="break-all text-slate-600">{"说明："}{runtimeData.lastEvent.detail}</div> : null}
                </div>
              ) : (
                <div className="text-slate-500">暂时还没有上游运行事件。</div>
              )}
            </div>

            {runtimeData.recentEvents.length > 0 ? (
              <div className="max-h-72 overflow-y-auto rounded-2xl border border-slate-200 bg-white">
                <table className="min-w-full text-sm">
                  <thead>
                    <tr className="border-b border-slate-200 text-left text-slate-400">
                      <th className="px-3 py-3">时间</th>
                      <th className="px-3 py-3">操作</th>
                      <th className="px-3 py-3">阶段</th>
                      <th className="px-3 py-3">上游 Key</th>
                      <th className="px-3 py-3">结果</th>
                    </tr>
                  </thead>
                  <tbody>
                    {[...runtimeData.recentEvents].reverse().map((event, index) => (
                      <tr key={`${event.at}-${event.operation}-${index}`} className="border-b border-slate-100 text-slate-700">
                        <td className="px-3 py-3 whitespace-nowrap">{new Date(event.at).toLocaleTimeString()}</td>
                        <td className="px-3 py-3">
                          <div className="font-medium text-slate-900">{event.operationLabel || event.operation}</div>
                          <div className="font-mono text-[11px] text-slate-500">{event.operation}</div>
                        </td>
                        <td className="px-3 py-3">
                          <div className="font-medium text-slate-900">{event.stageLabel || event.stage}</div>
                          <div className="font-mono text-[11px] text-slate-500">{event.stage}</div>
                        </td>
                        <td className="px-3 py-3">{event.upstreamKeyName || "-"}</td>
                        <td className="px-3 py-3">{event.success ? "成功" : "失败"}{event.httpStatus ? ` · ${event.httpStatus}` : ""}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      {data ? (
        <>
          <section className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
            <MetricCard label="上游 Key" value={`${data.summary.totalKeys}`} delta={`可用 ${data.summary.activeKeys}`} tone="accent" />
            <MetricCard label="基线检查" value={`${data.summary.healthyChecks}/${data.summary.healthyChecks + data.summary.unhealthyChecks}`} delta={`平均 ${Math.round(data.summary.avgLatencyMs)} ms`} tone={data.summary.unhealthyChecks ? "warning" : "success"} />
            <MetricCard label="全模型数量" value={`${data.fullSweep?.summary.total ?? 0}`} delta={`健康 ${data.fullSweep?.summary.healthy ?? 0}`} tone="neutral" />
            <MetricCard label="全模型平均延迟" value={`${Math.round(data.fullSweep?.summary.avgLatencyMs ?? 0)} ms`} delta={data.fullSweep ? `失败 ${data.fullSweep.summary.failed}` : "请先跑全部模型检查"} tone={data.fullSweep?.summary.failed ? "warning" : "success"} />
          </section>

          <section className="grid gap-6 xl:grid-cols-[1fr_1fr]">
            <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
              <CardHeader>
                <CardTitle>网关基线健康</CardTitle>
                <CardDescription>先检查 /models、默认聊天模型和默认 embedding 模型。</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                {(data.checks ?? []).map((check) => (
                  <div key={check.id} className="rounded-2xl border border-slate-200 bg-slate-50 p-4">
                    <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
                      <div>
                        <div className="flex items-center gap-3">
                          <div className="text-base font-medium text-slate-900">{check.title}</div>
                          <StatusBadge status={check.statusLabel} />
                        </div>
                        <div className="mt-2 font-mono text-xs text-slate-500">{check.method} {check.endpoint}</div>
                      </div>
                      <div className="text-right text-sm text-slate-600">
                        <div>HTTP {check.httpStatus || "-"}</div>
                        <div>{check.durationMs} ms</div>
                      </div>
                    </div>
                    <div className="mt-3 text-sm text-slate-600">{check.detail}</div>
                  </div>
                ))}
              </CardContent>
            </Card>

            <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
              <CardHeader>
                <CardTitle>说明</CardTitle>
                <CardDescription>这页探测的内容和执行顺序。</CardDescription>
              </CardHeader>
              <CardContent className="space-y-3 text-sm leading-7 text-slate-600">
                <div className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3">1. {"只有点击“立即检查”后，才会真实调用 NVIDIA 官方 "}<span className="font-mono">/models</span>{" 拉取模型目录。"}</div>
                <div className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3">2. {"chat 检测只等待首个流式输出块；embeddings 检测仍走标准 JSON 请求，用更轻的方式判断接口是否活着。"}</div>
                <div className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3">3. {"全模型检查按单并发执行，并建议使用“健康专用” Key，尽量降低对正常业务流量的影响。"}</div>
                <div className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3">{"当前探测使用的上游 key："}<span className="font-medium text-slate-900">{data.probeKeyName || "未选择"}</span>{" / "}{data.probeKeyDedicated ? "健康检查专用" : "复用业务 key"}{" / 探测超时 "}{data.probeTimeoutSecond || 0}{" 秒"}</div>
              </CardContent>
            </Card>
          </section>

          <section className="grid gap-6 xl:grid-cols-[0.9fr_1.1fr]">
            <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
              <CardHeader>
                <CardTitle>Key 状态分布</CardTitle>
                <CardDescription>帮助你快速判断当前上游 key 是否稳定。</CardDescription>
              </CardHeader>
              <CardContent>
                <HorizontalBarChart data={keyStatusData} barColor="from-sky-400 via-cyan-400 to-indigo-400" />
              </CardContent>
            </Card>

            <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
              <CardHeader>
                <CardTitle>基线检查延迟趋势</CardTitle>
                <CardDescription>显示最近几次基线检查的平均延迟。</CardDescription>
              </CardHeader>
              <CardContent>
                <SparkAreaChart data={historyData} valueFormatter={(value) => `${value} ms`} stroke="#38bdf8" fill="rgba(56, 189, 248, 0.15)" />
              </CardContent>
            </Card>
          </section>

          <section className="grid gap-6 xl:grid-cols-2">
            <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
              <CardHeader>
                <CardTitle>本次模型检查结果</CardTitle>
                <CardDescription>
                  {data.activeRun ? `范围：${data.activeRun.scope === "all" ? "全部模型" : data.activeRun.selectedModelId} / 模式：${renderRunProtocol(data.activeRun.protocol)}` : "当前还没有模型检查结果"}
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="grid gap-3 md:grid-cols-3">
                  <MetricCard label="模型数" value={`${data.activeRun?.summary.total ?? 0}`} tone="neutral" />
                  <MetricCard label="健康数" value={`${data.activeRun?.summary.healthy ?? 0}`} tone="success" />
                  <MetricCard label="平均延迟" value={`${Math.round(data.activeRun?.summary.avgLatencyMs ?? 0)} ms`} tone="accent" />
                </div>
                <div className="max-h-[420px] overflow-y-auto pr-2">
                  <HorizontalBarChart data={activeRunChart} barColor="from-violet-400 via-fuchsia-400 to-sky-400" emptyLabel="请先执行一次模型检查" />
                </div>
              </CardContent>
            </Card>

            <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
              <CardHeader>
                <CardTitle>最近一次全模型扫描</CardTitle>
                <CardDescription>总览页会消费这里的数据来绘制全模型图表。</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="grid gap-3 md:grid-cols-3">
                  <MetricCard label="模型数" value={`${data.fullSweep?.summary.total ?? 0}`} tone="neutral" />
                  <MetricCard label="健康数" value={`${data.fullSweep?.summary.healthy ?? 0}`} tone="success" />
                  <MetricCard label="失败数" value={`${data.fullSweep?.summary.failed ?? 0}`} tone={(data.fullSweep?.summary.failed ?? 0) > 0 ? "warning" : "accent"} />
                </div>
                <div className="max-h-[420px] overflow-y-auto pr-2">
                  <HorizontalBarChart data={fullSweepChart} barColor="from-cyan-400 via-sky-400 to-indigo-500" emptyLabel="请先执行一次“全部模型”检查" />
                </div>
              </CardContent>
            </Card>
          </section>

          <section className="grid gap-6 xl:grid-cols-2">
            <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
              <CardHeader>
                <CardTitle>本次成功模型</CardTitle>
                <CardDescription>列出这次检查中成功通过的模型。</CardDescription>
              </CardHeader>
              <CardContent>
                {activeRunSucceededModels.length === 0 ? (
                  <div className="rounded-xl border border-dashed border-slate-200 px-4 py-8 text-center text-sm text-slate-500">当前没有成功模型。</div>
                ) : (
                  <div className="space-y-3">
                    {activeRunSucceededModels.map((check, index) => (
                      <div key={`success-${check.protocol}-${check.modelId}-${index}`} className="rounded-2xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-800">
                        <div className="flex items-center justify-between gap-3">
                          <div className="font-mono text-xs text-emerald-900">{check.modelId}</div>
                          <StatusBadge status={check.statusLabel} />
                        </div>
                        <div className="mt-2 text-xs">协议：{check.protocol} · HTTP {check.httpStatus || '-'} · {check.durationMs} ms</div>
                        <div className="mt-2 text-sm break-all">{check.detail}</div>
                      </div>
                    ))}
                  </div>
                )}
              </CardContent>
            </Card>

            <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
              <CardHeader>
                <CardTitle>本次失败模型</CardTitle>
                <CardDescription>列出这次检查中失败或未通过的模型。</CardDescription>
              </CardHeader>
              <CardContent>
                {activeRunFailedModels.length === 0 ? (
                  <div className="rounded-xl border border-dashed border-slate-200 px-4 py-8 text-center text-sm text-slate-500">当前没有失败模型。</div>
                ) : (
                  <div className="space-y-3">
                    {activeRunFailedModels.map((check, index) => (
                      <div key={`failed-${check.protocol}-${check.modelId}-${index}`} className="rounded-2xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-800">
                        <div className="flex items-center justify-between gap-3">
                          <div className="font-mono text-xs text-rose-900">{check.modelId}</div>
                          <StatusBadge status={check.statusLabel} />
                        </div>
                        <div className="mt-2 text-xs">{"协议："}{renderRunProtocol(check.protocol)}{" / HTTP "}{check.httpStatus || '-'}{" / "}{check.durationMs}{" ms"}</div>
                        <div className="mt-2 text-sm break-all">{check.detail}</div>
                      </div>
                    ))}
                  </div>
                )}
              </CardContent>
            </Card>
          </section>

          <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
            <CardHeader>
              <CardTitle>模型检查明细</CardTitle>
              <CardDescription>每个模型都会记录协议、HTTP 状态、耗时和返回详情。</CardDescription>
            </CardHeader>
            <CardContent>
              {!activeRunChecks.length ? (
                <div className="rounded-xl border border-dashed border-slate-200 px-4 py-10 text-center text-sm text-slate-500">请先执行一次模型检查。</div>
              ) : (
                <div className="overflow-x-auto">
                  <table className="min-w-full text-sm">
                    <thead>
                      <tr className="border-b border-slate-200 text-left text-slate-400">
                        <th className="px-3 py-3">模型名</th>
                        <th className="px-3 py-3">协议</th>
                        <th className="px-3 py-3">状态</th>
                        <th className="px-3 py-3">HTTP</th>
                        <th className="px-3 py-3">耗时</th>
                        <th className="px-3 py-3">详情</th>
                        <th className="px-3 py-3">检查时间</th>
                      </tr>
                    </thead>
                    <tbody>
                      {activeRunChecks.map((check, index) => (
                        <tr key={`${check.protocol}-${check.modelId}-${index}`} className="border-b border-slate-100 align-top text-slate-700">
                          <td className="px-3 py-3 font-mono text-xs">{check.modelId}</td>
                          <td className="px-3 py-3">{renderRunProtocol(check.protocol)}</td>
                          <td className="px-3 py-3"><StatusBadge status={check.statusLabel} /></td>
                          <td className="px-3 py-3 font-mono">{check.httpStatus || "-"}</td>
                          <td className="px-3 py-3 font-mono">{check.durationMs} ms</td>
                          <td className="px-3 py-3 max-w-xl whitespace-pre-wrap break-all">{check.detail}</td>
                          <td className="px-3 py-3 whitespace-nowrap">{new Date(check.generatedAt).toLocaleString()}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </CardContent>
          </Card>

          <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
            <CardHeader>
              <CardTitle>建议</CardTitle>
              <CardDescription>根据当前检查结果给出的简单提示。</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              {(data.recommendations ?? []).map((item, index) => (
                <div key={`${index}-${item}`} className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-3 text-sm leading-6 text-slate-700">
                  <div className="mb-2 text-xs uppercase tracking-[0.2em] text-slate-400">建议 {index + 1}</div>
                  {item}
                </div>
              ))}
            </CardContent>
          </Card>

          <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
            <CardHeader>
              <CardTitle>基线接口耗时</CardTitle>
              <CardDescription>最近一次基线探测中，各官方接口的响应速度。</CardDescription>
            </CardHeader>
            <CardContent>
              <HorizontalBarChart data={baseLatencyData} barColor="from-fuchsia-400 via-violet-400 to-sky-400" />
            </CardContent>
          </Card>
        </>
      ) : null}
    </div>
  );
}


function renderRunProtocol(protocol: string) {
  switch (protocol) {
    case "chat":
      return "对话";
    case "embeddings":
      return "向量";
    case "auto":
      return "智能模式";
    default:
      return protocol;
  }
}
