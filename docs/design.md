# Hermes Design

## Goal

Hermes is a minimal AI API gateway for a single admin-controlled ToB/internal deployment.

It aggregates configurable OpenAI-compatible upstream channels and exposes a unified OpenAI-compatible API. The first version focuses on OpenRouter free models and Volcengine Doubao Coding Plan, both configured from the admin frontend.

## Non-Goals

- No public user registration
- No per-user API token management
- No billing or quota settlement
- No rate limiting in the first version
- No request log persistence in MongoDB in the first version
- No native Claude/Gemini protocol conversion in the first version

## Runtime Shape

```text
Client
  -> /v1/* Gateway API
  -> Gin
      -> MongoDB channels
      -> OpenAI-compatible upstream

Admin Browser
  -> React frontend served by Gin
  -> /api/* Admin API
  -> MongoDB channels
```

## Main Routes

Gateway:

- `GET /v1/models`
- `POST /v1/chat/completions`

Admin:

- `POST /api/admin/login`
- `GET /api/admin/me`
- `GET /api/status`
- `GET /api/channels`
- `POST /api/channels`
- `GET /api/channels/:id`
- `PUT /api/channels/:id`
- `DELETE /api/channels/:id`
- `POST /api/channels/:id/test`

## Channel Model

```json
{
  "name": "doubao-coding",
  "provider": "doubao_coding",
  "base_url": "https://ark.cn-beijing.volces.com/api/coding/v3",
  "api_key": "...",
  "models": ["coder"],
  "model_mapping": {
    "coder": "upstream-model-name"
  },
  "extra_headers": {},
  "strategy": {},
  "enabled": true,
  "priority": 0,
  "weight": 1
}
```

`models` are the external model names exposed by Hermes. `model_mapping` maps those names to upstream model names.

## Provider Presets

OpenRouter:

```text
base_url = https://openrouter.ai/api/v1
endpoint = /chat/completions
headers:
  Authorization: Bearer <api_key>
  HTTP-Referer: configured or default
  X-Title: configured or default
```

Doubao Coding Plan:

```text
base_url = https://ark.cn-beijing.volces.com/api/coding/v3
endpoint = /chat/completions
headers:
  Authorization: Bearer <api_key>
```

## Request Flow

1. `/v1/chat/completions` checks `GATEWAY_API_KEY`.
2. The handler reads the JSON body and extracts `model`.
3. Enabled channels whose `models` contain the requested model are loaded from MongoDB.
4. A channel is selected by priority and weight.
5. `model` is replaced with `model_mapping[model]` when present.
6. The request is forwarded to `{base_url}/chat/completions`.
7. JSON responses and SSE stream responses are passed through to the client.
8. If an upstream fails before a response is committed, Hermes retries the next matching channel.

## Deployment

Gin serves both API and frontend. In production, build the frontend into `web/dist`; Gin serves it as static files and falls back to `index.html`.

