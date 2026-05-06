"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import { cn } from "@/lib/utils";

const navItems = [
  ["\u603b\u89c8", "/admin"],
  ["API \u5bc6\u94a5", "/admin/keys"],
  ["\u4ee3\u7406\u6c60", "/admin/proxies"],
  ["\u81ea\u5b9a\u4e49 API Key", "/admin/master-keys"],
  ["\u7cfb\u7edf\u8bbe\u7f6e", "/admin/system"],
  ["\u5065\u5eb7\u68c0\u67e5", "/admin/health"],
  ["\u63a5\u53e3\u8c03\u8bd5", "/admin/debug"],
] as const;

function isActivePath(pathname: string, href: string) {
  if (href === "/admin") {
    return pathname === "/admin";
  }
  return pathname === href || pathname.startsWith(`${href}/`);
}

export default function AdminShell({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  const pathname = usePathname();

  return (
    <div className="h-screen overflow-hidden bg-[linear-gradient(180deg,#f8fafc_0%,#eef4ff_40%,#f8fafc_100%)] text-slate-900">
      <div className="pointer-events-none fixed inset-0 bg-[radial-gradient(circle_at_top_left,rgba(125,211,252,0.18),transparent_26%),radial-gradient(circle_at_bottom_right,rgba(191,219,254,0.22),transparent_28%)]" />
      <div className="relative flex h-full">
        <aside className="hidden h-screen w-72 shrink-0 border-r border-slate-200/70 bg-white/70 px-7 py-8 backdrop-blur-xl md:flex md:flex-col md:justify-between md:overflow-hidden">
          <div className="min-h-0">
            <div className="mb-10 flex items-center gap-4">
              <div className="grid h-11 w-11 place-items-center rounded-2xl border border-sky-100 bg-white shadow-[0_20px_60px_-35px_rgba(56,189,248,0.55)]">
                <div className="h-3 w-3 rounded-full bg-sky-400 shadow-[0_0_18px_rgba(56,189,248,0.8)]" />
              </div>
              <div>
                <h1 className="text-lg font-semibold tracking-wide text-slate-900">NVIDIA API 网关</h1>
                <p className="mt-1 text-xs tracking-[0.22em] text-slate-500">管理后台</p>
              </div>
            </div>

            <nav className="space-y-2 text-sm">
              {navItems.map(([label, href]) => {
                const active = isActivePath(pathname, href);
                return (
                  <Link
                    key={href}
                    href={href}
                    className={cn(
                      "group flex items-center justify-between rounded-2xl border px-4 py-3 transition-all",
                      active
                        ? "border-sky-200 bg-sky-50 text-slate-900 shadow-sm"
                        : "border-transparent text-slate-600 hover:border-slate-200 hover:bg-white hover:text-slate-900 hover:shadow-sm",
                    )}
                  >
                    <span className={cn("font-medium", active ? "text-slate-900" : undefined)}>{label}</span>
                    <span className={cn("text-[11px] transition", active ? "text-sky-600" : "text-slate-400 group-hover:text-sky-500")}>
                      {active ? "当前" : "进入"}
                    </span>
                  </Link>
                );
              })}
            </nav>
          </div>

          <div className="rounded-2xl border border-slate-200/70 bg-white/80 p-4 text-xs text-slate-500 shadow-sm">
            <div className="font-medium uppercase tracking-[0.22em] text-slate-400">版本</div>
            <div className="mt-2 text-sm text-slate-800">v3.0.0</div>
            <div className="mt-2 leading-6">统一管理上游 key、网关 API Key、协议出口和健康检查。</div>
          </div>
        </aside>

        <main className="relative h-screen flex-1 overflow-y-auto">
          <div className="mx-auto min-h-full w-full max-w-[1680px] px-6 py-8 md:px-10 xl:px-14">
            <header className="sticky top-0 z-20 mb-8 rounded-[28px] border border-slate-200/70 bg-white/85 px-6 py-6 shadow-sm backdrop-blur-xl md:flex md:items-end md:justify-between">
              <div>
                <div className="text-xs uppercase tracking-[0.28em] text-slate-400">运行概览</div>
                <h2 className="mt-3 text-3xl font-semibold tracking-tight text-slate-900 md:text-4xl">网关管理</h2>
                <p className="mt-3 max-w-2xl text-sm leading-6 text-slate-500">在这里查看网关状态、管理上游 key、配置协议出口，并做接口调试与健康检查。</p>
              </div>
              <div className="mt-6 flex items-center gap-3 rounded-full border border-emerald-200 bg-emerald-50 px-4 py-2 text-sm text-emerald-700 shadow-sm md:mt-0">
                <span className="relative flex h-2.5 w-2.5">
                  <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
                  <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-emerald-500" />
                </span>
                系统在线
              </div>
            </header>

            {children}
          </div>
        </main>
      </div>
    </div>
  );
}
