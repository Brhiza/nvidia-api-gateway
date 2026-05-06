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

export async function POST(
  _request: Request,
  { params }: { params: Promise<{ id: string }> },
) {
  try {
    const { id } = await params;
    const res = await fetch(`${backend}/admin/keys/${id}/probe`, {
      method: 'POST',
      cache: 'no-store',
    });

    return forwardJson(res, '探测密钥状态失败。');
  } catch {
    return NextResponse.json({ error: '探测密钥状态失败。' }, { status: 500 });
  }
}
