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
    const [keysRes, statsRes] = await Promise.all([
      fetch(`${backend}/admin/keys`, { cache: 'no-store' }),
      fetch(`${backend}/admin/system/stats`, { cache: 'no-store' }),
    ]);

    if (!keysRes.ok || !statsRes.ok) {
      return NextResponse.json(
        { error: '后端接口不可用，请检查网关与 Redis 是否已启动。' },
        { status: 502 },
      );
    }

    const [{ keys }, stats] = await Promise.all([keysRes.json(), statsRes.json()]);

    return NextResponse.json({ keys, stats });
  } catch {
    return NextResponse.json({ error: '获取数据失败。' }, { status: 500 });
  }
}

export async function POST(request: Request) {
  try {
    const body = await request.json();
    const res = await fetch(`${backend}/admin/keys`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      cache: 'no-store',
      body: JSON.stringify(body),
    });

    return forwardJson(res, '新增密钥失败。');
  } catch {
    return NextResponse.json({ error: '新增密钥失败。' }, { status: 500 });
  }
}
