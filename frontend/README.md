# NVIDIA API Gateway Frontend

## One-command startup

From the `nvidia-api-gateway` directory, run:

```bash
go run main.go serve-all
```

This starts:
- backend on `http://localhost:18080`
- frontend on `http://localhost:14000`

The frontend now runs in Webpack mode intentionally to avoid a Next.js 16 + Windows local memory crash seen with the default dev/build path in this repo.

Then open:
- `http://localhost:14000`

## Manual startup

Backend default port:
- `18080`

Frontend default port:
- `14000`

Optional environment variables:
- `BACKEND_PORT=18080`
- `PORT=18080` (legacy fallback for backend)
- `FRONTEND_PORT=14000`
- `API_BASE_URL=http://localhost:18080`
- `PUBLIC_GATEWAY_BASE_URL=http://127.0.0.1:18080` (used by the admin UI to show the real gateway base URL)

Start the frontend:

```bash
npm run dev
```

The frontend talks to the backend through Next.js route handlers. If the backend is not running on the same machine/port, set:

```bash
API_BASE_URL=http://localhost:18080
```

Build for production:

```bash
npm run build
npm run start
```

## Recommended first-run verification

- New installations now recommend using a **custom API key** instead of anonymous access.
- The real gateway base URL is the backend address, not the frontend admin address.
- Recommended verification order:

```bash
curl http://127.0.0.1:18080/v1/models \
  -H "Authorization: Bearer <your-custom-api-key>"
```

Then verify chat:

```bash
curl http://127.0.0.1:18080/v1/chat/completions \
  -H "Authorization: Bearer <your-custom-api-key>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "hello"}],
    "stream": false
  }'
```
