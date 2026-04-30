# Hermes

Hermes is a small single-tenant AI gateway for internal or ToB use.

It provides:

- OpenAI-compatible `/v1/models` and `/v1/chat/completions`
- Admin login and channel management UI
- MongoDB-backed channel configuration
- OpenAI-compatible upstream proxying for OpenRouter, Doubao Coding Plan, and custom providers
- Normal JSON and SSE streaming passthrough

It intentionally does not include user management, billing, per-user tokens, rate limiting, or request log persistence in the first version.

## Quick Start

```bash
cp .env.example .env
go run ./cmd/server
```

Frontend development:

```bash
cd web
bun install
bun run dev
```

Production frontend build:

```bash
cd web
bun run build
```

Gin serves `web/dist` when it exists, so the backend and frontend can be deployed as one service.

