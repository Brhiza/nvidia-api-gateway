import { NextResponse } from 'next/server';

const backend = process.env.API_BASE_URL || 'http://localhost:18080';

async function forwardJson(res: Response, fallbackMessage: string) {
  const text = await res.text();
  if (!text) {
    return NextResponse.json({ success: res.ok }, { status: res.status });
  }

  try {
    return NextResponse.json(JSON.parse(text), { status: res.status });
  } catch {
    return NextResponse.json({ error: fallbackMessage, detail: text }, { status: res.status });
  }
}

export async function GET() {
  try {
    const res = await fetch(`${backend}/admin/health/report`, { cache: 'no-store' });
    return forwardJson(res, '读取系统健康报告失败。');
  } catch {
    return NextResponse.json({ error: '读取系统健康报告失败。' }, { status: 500 });
  }
}

export async function POST(request: Request) {
  try {
    const body = await request.text();
    const res = await fetch(`${backend}/admin/health/report/run`, {
      method: 'POST',
      headers: body ? { 'Content-Type': 'application/json' } : undefined,
      cache: 'no-store',
      body: body || undefined,
    });
    return forwardJson(res, '执行系统健康检查失败。');
  } catch {
    return NextResponse.json({ error: '执行系统健康检查失败。' }, { status: 500 });
  }
}
