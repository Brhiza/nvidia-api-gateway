"use client";

import Link from "next/link";
import useSWR from "swr";

import { HorizontalBarChart, MetricCard, SparkAreaChart, StatusBadge } from "@/components/dashboard/visuals";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

const fetcher = async (url: string) => {
  const res = await fetch(url, { cache: "no-store" });
  const data = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error(data?.error || "request_failed");
  }
  return data;
};

type KeysData = {
  keys: Array<{ id: number; name: string; weight: number; status: string; updatedAt: string }>;
  stats: { active: number; cooling: number; dead: number };
};

type MasterKeysData = {
  keys: Array<{ id: number; name: string; status: string; rpm: number; tpm: number; quota: number }>;
};

type SystemConfig = {
  upstreamBaseURL: string;
  gatewayBaseURL: string;
  maxRetries: number;
  maxConcurrency: number;
  enableOpenAI: boolean;
  enableClaude: boolean;
  enableGemini: boolean;
  anonymousAccess: boolean;
};

type HealthReport = {
  generatedAt: string;
  probeKeyName: string;
  summary: {
    overallStatus: string;
    healthyChecks: number;
    unhealthyChecks: number;
    avgLatencyMs: number;
    totalKeys: number;
    activeKeys: number;
  };
  history: Array<{ label: string; avgLatencyMs: number }>;
  checks: Array<{ title: string; durationMs: number; statusLabel: string }>;
  fullSweep?: {
    summary: { total: number; healthy: number; failed: number; avgLatencyMs: number };
    latencyChart: Array<{ label: string; value: number; meta?: string }>;
  } | null;
};

const quickLinks = [
  {
    title: "API 密钥",
    description: "添加、批量导入、探测状态和调整权重。",
    href: "/admin/keys",
  },
  {
    title: "自定义 API Key",
    description: "给下游客户端分配网关鉴权 Key，并支持轮换。",
    href: "/admin/master-keys",
  },
  {
    title: "健康检查",
    description: "真实访问 NVIDIA 官方接口，检查所有模型与延迟。",
    href: "/admin/health",
  },
  {
    title: "接口调试",
    description: "获取全部模型，逐模型测试 OpenAI / Claude / Gemini。",
    href: "/admin/debug",
  },
];

export default function AdminOverview() {
  const { data: keysData } = useSWR<KeysData>("/api/keys", fetcher);
  const { data: masterKeysData } = useSWR<MasterKeysData>("/api/master-keys", fetcher);
  const { data: systemConfig } = useSWR<SystemConfig>("/api/system/config", fetcher);
  const { data: healthReport } = useSWR<HealthReport>("/api/health/report", fetcher);

  const keyWeightData = (keysData?.keys ?? [])
    .slice()
    .sort((a, b) => b.weight - a.weight)
    .map((key) => ({ label: key.name, value: Number(key.weight.toFixed(2)), meta: key.status }))
    .slice(0, 6);

  const protocolData = [
    { label: "OpenAI", value: systemConfig?.enableOpenAI ? 1 : 0, meta: systemConfig?.enableOpenAI ? "已开启" : "已关闭" },
    { label: "Claude", value: systemConfig?.enableClaude ? 1 : 0, meta: systemConfig?.enableClaude ? "已开启" : "已关闭" },
    { label: "Gemini", value: systemConfig?.enableGemini ? 1 : 0, meta: systemConfig?.enableGemini ? "已开启" : "已关闭" },
  ];

  const latencyTrend = (Array.isArray(healthReport?.history) ? healthReport.history : []).map((item) => ({ label: item.label, value: Math.round(item.avgLatencyMs) }));
  const endpointLatency = (healthReport?.checks ?? []).map((item) => ({ label: item.title.replace("NVIDIA 官方 ", ""), value: item.durationMs, meta: item.statusLabel }));
  const fullSweepChart = Array.isArray(healthReport?.fullSweep?.latencyChart) ? healthReport.fullSweep.latencyChart : [];

  return (
    <div className="space-y-6">
      <section className="rounded-[30px] border border-slate-200/70 bg-[radial-gradient(circle_at_top_left,rgba(191,219,254,0.7),transparent_28%),linear-gradient(180deg,rgba(255,255,255,0.95),rgba(248,250,252,0.92))] p-8 shadow-sm">
        <div className="flex flex-col gap-6 xl:flex-row xl:items-end xl:justify-between">
          <div>
            <div className="text-xs uppercase tracking-[0.28em] text-slate-400">今日概览</div>
            <h1 className="mt-3 text-3xl font-semibold tracking-tight text-slate-900 md:text-5xl">网关运行情况</h1>
            <p className="mt-4 max-w-3xl text-sm leading-7 text-slate-500">
              这里集中展示上游 key、自定义 API Key、协议开关和全模型健康检查结果。日常运维时，先看这一页就能快速判断网关是否正常。
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            {healthReport ? <StatusBadge status={healthReport.summary.overallStatus} className="text-xs" /> : null}
            <div className="rounded-full border border-slate-200 bg-white px-4 py-2 text-sm text-slate-600 shadow-sm">
              最近检查：{healthReport ? new Date(healthReport.generatedAt).toLocaleTimeString() : "暂未执行"}
            </div>
          </div>
        </div>
      </section>

      <section className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <MetricCard label="上游 Key" value={`${healthReport?.summary.totalKeys ?? keysData?.keys.length ?? 0}`} delta={`可用 ${healthReport?.summary.activeKeys ?? keysData?.stats.active ?? 0}`} tone="accent" />
        <MetricCard label="自定义 API Key" value={`${masterKeysData?.keys.length ?? 0}`} delta={`匿名访问：${systemConfig?.anonymousAccess ? "开启" : "关闭"}`} tone="neutral" />
        <MetricCard label="全模型数量" value={`${healthReport?.fullSweep?.summary.total ?? 0}`} delta={`健康 ${healthReport?.fullSweep?.summary.healthy ?? 0}`} tone="success" />
        <MetricCard label="全模型平均延迟" value={`${Math.round(healthReport?.fullSweep?.summary.avgLatencyMs ?? 0)} ms`} delta={healthReport?.fullSweep ? `失败 ${healthReport.fullSweep.summary.failed}` : "请先做全部模型检查"} tone={healthReport?.fullSweep?.summary.failed ? "warning" : "accent"} />
      </section>

      <section className="grid gap-6 xl:grid-cols-[1.2fr_0.8fr]">
        <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
          <CardHeader>
            <CardTitle>健康检查延迟趋势</CardTitle>
            <CardDescription>显示最近几次基线探测的平均延迟。</CardDescription>
          </CardHeader>
          <CardContent>
            <SparkAreaChart data={latencyTrend} valueFormatter={(value) => `${value} ms`} stroke="#38bdf8" fill="rgba(56, 189, 248, 0.15)" />
          </CardContent>
        </Card>

        <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
          <CardHeader>
            <CardTitle>协议开关</CardTitle>
            <CardDescription>当前开放给下游调用的协议类型。</CardDescription>
          </CardHeader>
          <CardContent>
            <HorizontalBarChart data={protocolData} barColor="from-sky-400 via-cyan-400 to-indigo-400" emptyLabel="暂无协议配置" />
          </CardContent>
        </Card>
      </section>

      <section className="grid gap-6 xl:grid-cols-2">
        <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
          <CardHeader>
            <CardTitle>全模型延迟检测</CardTitle>
            <CardDescription>最近一次“全部模型”健康检查的图表数据。</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="max-h-[420px] overflow-y-auto pr-2">
              <HorizontalBarChart data={fullSweepChart} barColor="from-cyan-400 via-sky-400 to-indigo-500" emptyLabel="请先到健康检查页执行一次“全部模型”检查" />
            </div>
          </CardContent>
        </Card>

        <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
          <CardHeader>
            <CardTitle>官方接口耗时</CardTitle>
            <CardDescription>最近一次基线探测中，各官方接口的响应速度。</CardDescription>
          </CardHeader>
          <CardContent>
            <HorizontalBarChart data={endpointLatency} barColor="from-violet-400 via-fuchsia-400 to-sky-400" emptyLabel="请先到健康检查页执行一次探测" />
          </CardContent>
        </Card>
      </section>

      <section className="grid gap-6 xl:grid-cols-2">
        <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
          <CardHeader>
            <CardTitle>Key 权重分布</CardTitle>
            <CardDescription>看看当前哪些上游 key 承担的流量更多。</CardDescription>
          </CardHeader>
          <CardContent>
            <HorizontalBarChart data={keyWeightData} barColor="from-cyan-400 via-sky-400 to-indigo-500" emptyLabel="当前还没有 key 数据" />
          </CardContent>
        </Card>

        <Card className="border border-slate-200/70 bg-white/90 shadow-sm">
          <CardHeader>
            <CardTitle>真实网关出口</CardTitle>
            <CardDescription>给 OpenAI/Claude/Gemini 客户端填写的真实调用地址。</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="rounded-2xl border border-slate-200 bg-slate-50 px-4 py-4 text-sm leading-7 text-slate-700">
              <div>OpenAI 客户端 <span className="font-mono">base_url</span> 请填写：</div>
              <div className="mt-2 break-all font-mono text-slate-900">{systemConfig?.gatewayBaseURL ? `${systemConfig.gatewayBaseURL}/v1` : "http://127.0.0.1:18080/v1"}</div>
            </div>
          </CardContent>
        </Card>
      </section>

      <section className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        {quickLinks.map((item) => (
          <Link
            key={item.href}
            href={item.href}
            className="rounded-[24px] border border-slate-200/70 bg-white/90 p-5 text-slate-900 shadow-sm transition-all hover:-translate-y-0.5 hover:border-sky-200 hover:shadow-md"
          >
            <div className="text-sm font-semibold">{item.title}</div>
            <p className="mt-3 text-sm leading-6 text-slate-500">{item.description}</p>
            <div className="mt-4 text-xs uppercase tracking-[0.2em] text-sky-500">打开</div>
          </Link>
        ))}
      </section>
    </div>
  );
}
