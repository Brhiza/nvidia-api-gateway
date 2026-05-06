import { NextResponse } from 'next/server';

type DebugGatewayRequest = {
  path: string;
  method: string;
  token?: string;
  tokenHeader?: 'authorization' | 'x-api-key' | 'x-goog-api-key';
  headers?: Record<string, string>;
  body?: string;
};

const backend = process.env.API_BASE_URL || 'http://localhost:18080';

function sanitizePath(raw: string) {
  const value = (raw || '').trim();
  if (!value.startsWith('/')) {
    throw new Error('path must start with /');
  }
  return value;
}

function sanitizeMethod(raw: string) {
  const method = (raw || 'GET').trim().toUpperCase();
  if (!['GET', 'POST', 'PUT', 'PATCH', 'DELETE'].includes(method)) {
    throw new Error('unsupported method');
  }
  return method;
}

function normalizeHeaders(headers?: Record<string, string>) {
  const result = new Headers();
  for (const [key, value] of Object.entries(headers || {})) {
    const headerName = key.trim();
    const headerValue = (value || '').trim();
    if (!headerName || !headerValue) continue;
    result.set(headerName, headerValue);
  }
  return result;
}

function decodeBase64URL(value: string) {
  const normalized = value.replace(/-/g, '+').replace(/_/g, '/');
  const padding = normalized.length % 4;
  const padded = padding === 0 ? normalized : normalized + '='.repeat(4 - padding);
  return Buffer.from(padded, 'base64').toString('utf8');
}

function decodeGatewayDebugHeaders(headers: Record<string, string>) {
  const encodedToPlain: Record<string, string> = {
    'x-gateway-upstream-key-name-b64': 'x-gateway-upstream-key-name',
    'x-gateway-upstream-key-chain-b64': 'x-gateway-upstream-key-chain',
    'x-gateway-upstream-last-error-b64': 'x-gateway-upstream-last-error',
  };

  for (const [encodedKey, plainKey] of Object.entries(encodedToPlain)) {
    const encodedValue = headers[encodedKey];
    if (!encodedValue) continue;
    try {
      headers[plainKey] = decodeBase64URL(encodedValue);
    } catch {
      // ignore invalid companion header and keep the raw value
    }
  }
}

export async function POST(request: Request) {
  const startedAt = Date.now();
  try {
    const payload = (await request.json()) as DebugGatewayRequest;
    const path = sanitizePath(payload.path);
    const method = sanitizeMethod(payload.method);
    const upstreamHeaders = normalizeHeaders(payload.headers);

    const token = (payload.token || '').trim();
    const tokenHeader = payload.tokenHeader || 'authorization';
    if (token) {
      if (tokenHeader === 'authorization') {
        upstreamHeaders.set('Authorization', `Bearer ${token}`);
      } else if (tokenHeader === 'x-api-key') {
        upstreamHeaders.set('x-api-key', token);
      } else if (tokenHeader === 'x-goog-api-key') {
        upstreamHeaders.set('x-goog-api-key', token);
      }
    }

    let body: string | undefined;
    if (payload.body && method !== 'GET') {
      body = payload.body;
      if (!upstreamHeaders.has('Content-Type')) {
        upstreamHeaders.set('Content-Type', 'application/json');
      }
    }

    const response = await fetch(`${backend}${path}`, {
      method,
      headers: upstreamHeaders,
      cache: 'no-store',
      body,
    });

    const text = await response.text();
    const durationMs = Date.now() - startedAt;
    const headerEntries = Object.fromEntries(response.headers.entries());
    decodeGatewayDebugHeaders(headerEntries);

    let json: unknown = null;
    try {
      json = text ? JSON.parse(text) : null;
    } catch {
      json = null;
    }

    const looksLikeSSEFailure =
      (response.headers.get('content-type') || '').includes('text/event-stream') &&
      (text.includes('event: response.failed') || text.includes('"type":"response.failed"'));

    return NextResponse.json({
      ok: response.ok && !looksLikeSSEFailure,
      status: looksLikeSSEFailure ? 502 : response.status,
      statusText: response.statusText,
      contentType: response.headers.get('content-type') || '',
      durationMs,
      headers: headerEntries,
      bodyText: text,
      bodyJson: json,
    });
  } catch (error) {
    return NextResponse.json(
      {
        ok: false,
        status: 500,
        statusText: 'Debug proxy error',
        contentType: 'application/json',
        durationMs: Date.now() - startedAt,
        headers: {},
        bodyText: '',
        bodyJson: null,
        error: error instanceof Error ? error.message : 'unknown_error',
      },
      { status: 500 },
    );
  }
}
