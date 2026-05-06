"use client";

import Link from "next/link";

export default function Home() {
  return (
    <div className="min-h-screen bg-[linear-gradient(180deg,#f8fafc_0%,#eef4ff_42%,#f8fafc_100%)] px-6 py-10 text-slate-900">
      <div className="mx-auto flex min-h-[calc(100vh-5rem)] max-w-6xl items-center">
        <div className="grid w-full gap-10 xl:grid-cols-[1.1fr_0.9fr] xl:items-center">
          <div>
            <div className="inline-flex items-center rounded-full border border-sky-100 bg-white px-4 py-2 text-xs tracking-[0.22em] text-sky-600 shadow-sm">
              NVIDIA API 网关
            </div>
            <h1 className="mt-6 text-4xl font-semibold tracking-tight text-slate-900 md:text-6xl">
              统一接入、统一调度、统一管理
            </h1>
            <p className="mt-6 max-w-2xl text-base leading-8 text-slate-500 md:text-lg">
              这个后台用来管理上游 NVIDIA key、下游访问令牌、协议出口、健康检查和接口调试。日常使用时，先进入后台看状态，再按需要去各个页面操作就可以了。
            </p>
            <div className="mt-8 flex flex-wrap gap-4">
              <Link href="/admin" className="inline-flex items-center rounded-2xl bg-slate-900 px-6 py-3 text-sm font-medium text-white shadow-sm transition hover:bg-slate-800">
                打开后台
              </Link>
              <Link href="/admin/health" className="inline-flex items-center rounded-2xl border border-slate-200 bg-white px-6 py-3 text-sm font-medium text-slate-700 shadow-sm transition hover:border-sky-200 hover:text-slate-900">
                查看健康检查
              </Link>
            </div>
          </div>

          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-1">
            <div className="rounded-[28px] border border-slate-200/70 bg-white/90 p-6 shadow-sm">
              <div className="text-xs uppercase tracking-[0.22em] text-slate-400">上游管理</div>
              <div className="mt-3 text-xl font-semibold text-slate-900">录入 NVIDIA key</div>
              <p className="mt-3 text-sm leading-7 text-slate-500">支持单个录入、批量导入、状态探测和权重调整。</p>
            </div>
            <div className="rounded-[28px] border border-slate-200/70 bg-white/90 p-6 shadow-sm">
              <div className="text-xs uppercase tracking-[0.22em] text-slate-400">下游接入</div>
              <div className="mt-3 text-xl font-semibold text-slate-900">管理访问令牌</div>
              <p className="mt-3 text-sm leading-7 text-slate-500">可以创建、复制、轮换和停用访问令牌，方便分配给不同系统。</p>
            </div>
            <div className="rounded-[28px] border border-slate-200/70 bg-white/90 p-6 shadow-sm">
              <div className="text-xs uppercase tracking-[0.22em] text-slate-400">系统检查</div>
              <div className="mt-3 text-xl font-semibold text-slate-900">真实探测官方接口</div>
              <p className="mt-3 text-sm leading-7 text-slate-500">直接访问 NVIDIA 官方 API，检查模型、对话和 Embeddings 是否正常。</p>
            </div>
            <div className="rounded-[28px] border border-slate-200/70 bg-white/90 p-6 shadow-sm">
              <div className="text-xs uppercase tracking-[0.22em] text-slate-400">接口调试</div>
              <div className="mt-3 text-xl font-semibold text-slate-900">在线发请求看结果</div>
              <p className="mt-3 text-sm leading-7 text-slate-500">支持 OpenAI、Claude、Gemini、Responses 和 Embeddings 的在线验证。</p>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
