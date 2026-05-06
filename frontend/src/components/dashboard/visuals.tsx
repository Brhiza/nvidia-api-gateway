"use client";

import { cn } from "@/lib/utils";

export function StatusBadge({ status, className }: { status: string; className?: string }) {
  const normalized = status.toLowerCase();
  const styles =
    normalized === "healthy" || normalized === "active"
      ? "border-emerald-200 bg-emerald-50 text-emerald-700"
      : normalized === "degraded" || normalized === "cooling"
        ? "border-amber-200 bg-amber-50 text-amber-700"
        : normalized === "critical" || normalized === "dead" || normalized === "failed"
          ? "border-rose-200 bg-rose-50 text-rose-700"
          : "border-sky-200 bg-sky-50 text-sky-700";
  return (
    <span className={cn("inline-flex items-center rounded-full border px-2.5 py-1 text-[11px] font-medium uppercase tracking-[0.18em]", styles, className)}>
      {status}
    </span>
  );
}

export function MetricCard({ label, value, delta, tone = "neutral" }: { label: string; value: string; delta?: string; tone?: "neutral" | "accent" | "success" | "warning" }) {
  const toneClass = {
    neutral: "from-white to-slate-50",
    accent: "from-sky-50 to-indigo-50",
    success: "from-emerald-50 to-emerald-100/40",
    warning: "from-amber-50 to-orange-50",
  }[tone];

  return (
    <div className={cn("rounded-2xl border border-slate-200/70 bg-gradient-to-br p-5 text-slate-900 shadow-sm", toneClass)}>
      <div className="text-xs uppercase tracking-[0.22em] text-slate-400">{label}</div>
      <div className="mt-3 text-3xl font-semibold tracking-tight text-slate-900">{value}</div>
      {delta ? <div className="mt-2 text-xs text-slate-500">{delta}</div> : null}
    </div>
  );
}

export function HorizontalBarChart({ data, barColor = "from-sky-400 to-cyan-300", emptyLabel = "暂无数据" }: { data: { label: string; value: number; meta?: string }[]; barColor?: string; emptyLabel?: string }) {
  const max = Math.max(...data.map((item) => item.value), 0);
  if (data.length === 0) {
    return <div className="rounded-xl border border-dashed border-slate-200 px-4 py-8 text-center text-sm text-slate-400">{emptyLabel}</div>;
  }
  return (
    <div className="space-y-4">
      {data.map((item, index) => {
        const width = max > 0 ? Math.max((item.value / max) * 100, 4) : 0;
        return (
          <div key={`${item.label}-${index}`} className="space-y-2">
            <div className="flex items-center justify-between gap-3 text-sm">
              <div className="truncate text-slate-700">{item.label}</div>
              <div className="flex shrink-0 items-center gap-3 text-slate-400">
                {item.meta ? <span className="text-xs uppercase tracking-[0.18em]">{item.meta}</span> : null}
                <span className="font-mono text-xs text-slate-800">{item.value}</span>
              </div>
            </div>
            <div className="h-2 rounded-full bg-slate-100">
              <div className={cn("h-2 rounded-full bg-gradient-to-r", barColor)} style={{ width: `${width}%` }} />
            </div>
          </div>
        );
      })}
    </div>
  );
}

export function SparkAreaChart({ data, stroke = "#38bdf8", fill = "rgba(56, 189, 248, 0.18)", valueFormatter = (value: number) => `${value}` }: { data: { label: string; value: number }[]; stroke?: string; fill?: string; valueFormatter?: (value: number) => string }) {
  if (data.length === 0) {
    return <div className="rounded-xl border border-dashed border-slate-200 px-4 py-8 text-center text-sm text-slate-400">暂无趋势数据</div>;
  }
  const width = 480;
  const height = 180;
  const padding = 18;
  const values = data.map((item) => item.value);
  const max = Math.max(...values, 1);
  const min = Math.min(...values, 0);
  const range = Math.max(max - min, 1);
  const stepX = data.length > 1 ? (width - padding * 2) / (data.length - 1) : 0;
  const points = data.map((item, index) => {
    const x = padding + index * stepX;
    const normalized = (item.value - min) / range;
    const y = height - padding - normalized * (height - padding * 2);
    return { ...item, x, y };
  });
  const linePath = points.map((point, index) => `${index === 0 ? "M" : "L"}${point.x},${point.y}`).join(" ");
  const areaPath = `${linePath} L ${points.at(-1)?.x ?? padding},${height - padding} L ${points[0]?.x ?? padding},${height - padding} Z`;

  return (
    <div className="space-y-3">
      <svg viewBox={`0 0 ${width} ${height}`} className="h-48 w-full overflow-visible">
        <defs>
          <linearGradient id="spark-gradient" x1="0" x2="0" y1="0" y2="1">
            <stop offset="0%" stopColor={stroke} stopOpacity="0.28" />
            <stop offset="100%" stopColor={stroke} stopOpacity="0.03" />
          </linearGradient>
        </defs>
        <path d={areaPath} fill="url(#spark-gradient)" />
        <path d={linePath} fill="none" stroke={stroke} strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
        {points.map((point, index) => (
          <g key={`${point.label}-${index}`}>
            <circle cx={point.x} cy={point.y} r="4" fill={stroke} />
            <circle cx={point.x} cy={point.y} r="10" fill={fill} />
          </g>
        ))}
      </svg>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-6">
        {points.map((point, index) => (
          <div key={`${point.label}-${index}`} className="rounded-xl border border-slate-200 bg-slate-50 px-3 py-2">
            <div className="text-[11px] uppercase tracking-[0.18em] text-slate-400">{point.label}</div>
            <div className="mt-1 text-sm font-medium text-slate-800">{valueFormatter(point.value)}</div>
          </div>
        ))}
      </div>
    </div>
  );
}

export function RingChart({ title, value, total, subtitle, color = "#38bdf8" }: { title: string; value: number; total: number; subtitle?: string; color?: string }) {
  const safeTotal = Math.max(total, 1);
  const percent = Math.max(0, Math.min(100, (value / safeTotal) * 100));
  return (
    <div className="rounded-2xl border border-slate-200/70 bg-white p-5 shadow-sm">
      <div className="text-xs uppercase tracking-[0.2em] text-slate-400">{title}</div>
      <div className="mt-4 flex items-center gap-5">
        <div className="grid h-24 w-24 place-items-center rounded-full" style={{ background: `conic-gradient(${color} ${percent}%, rgba(148, 163, 184, 0.12) ${percent}% 100%)` }}>
          <div className="grid h-16 w-16 place-items-center rounded-full bg-white text-sm font-semibold text-slate-900">{Math.round(percent)}%</div>
        </div>
        <div>
          <div className="text-3xl font-semibold tracking-tight text-slate-900">{value}</div>
          <div className="mt-2 text-sm text-slate-500">{subtitle ?? `总量 ${total}`}</div>
        </div>
      </div>
    </div>
  );
}
