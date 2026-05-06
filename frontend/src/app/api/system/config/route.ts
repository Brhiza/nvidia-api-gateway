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
    const res = await fetch(`${backend}/admin/system/config`, { cache: 'no-store' });
    return forwardJson(res, '读取系统配置失败。');
  } catch {
    return NextResponse.json({ error: '读取系统配置失败。' }, { status: 500 });
  }
}

export async function PUT(request: Request) {
  try {
    const body = await request.json();
    const res = await fetch(`${backend}/admin/system/config`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      cache: 'no-store',
      body: JSON.stringify(body),
    });

    return forwardJson(res, '保存系统配置失败。');
  } catch {
    return NextResponse.json({ error: '保存系统配置失败。' }, { status: 500 });
  }
}
