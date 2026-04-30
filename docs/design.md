# Hermes Design

## Goal

Hermes is a minimal AI API gateway for a single admin-controlled ToB/internal deployment.

It aggregates configurable OpenAI-compatible upstream channels and exposes a unified OpenAI-compatible API. Administrators configure upstream channels, API users, and per-user gateway tokens from the admin surface.

## Non-Goals

- No public user registration
- No billing or quota settlement
- No rate limiting in the first version
- No request log persistence in MongoDB in the first version
- No native Claude/Gemini protocol conversion in the first version

## Runtime Shape

```text
Client
  -> /v1/* Gateway API
  -> Gin
      -> MongoDB users/tokens
      -> MongoDB channels
      -> OpenAI-compatible upstream

Admin Browser
  -> React frontend served by Gin
  -> /api/* Admin API
  -> MongoDB channels/users/tokens
```

## Main Routes

Gateway:

- `GET /v1/models`
- `POST /v1/chat/completions`

Admin:

- `POST /api/admin/login`
- `GET /api/admin/me`
- `GET /api/status`
- `GET /api/barometer`
- `GET /api/channels`
- `POST /api/channels`
- `GET /api/channels/:id`
- `PUT /api/channels/:id`
- `DELETE /api/channels/:id`
- `POST /api/channels/:id/test`
- `GET /api/users`
- `POST /api/users`
- `GET /api/users/:id`
- `PUT /api/users/:id`
- `DELETE /api/users/:id`
- `GET /api/users/:id/tokens`
- `POST /api/users/:id/tokens`
- `GET /api/users/:id/tokens/:token_id`
- `PUT /api/users/:id/tokens/:token_id`
- `DELETE /api/users/:id/tokens/:token_id`

## Channel Model

```json
{
  "name": "doubao-coding",
  "provider": "doubao_coding",
  "base_url": "https://ark.cn-beijing.volces.com/api/coding/v3",
  "api_key": "...",
  "models": ["deepseek-v3"],
  "model_mappings": {
    "deepseek-v3": ["deepseek-v3-2-251201", "deepseek-v3-1-250821"]
  },
  "model_mapping": {
    "deepseek-v3": "deepseek-v3-2-251201"
  },
  "extra_headers": {},
  "strategy": {},
  "enabled": true,
  "priority": 0,
  "weight": 1
}
```

`models` are the external model names exposed by Hermes. `model_mappings` maps one external name to one or more upstream models inside the same channel. `model_mapping` is kept as a compatibility field and stores the first upstream model only.

## User And Token Model

Hermes follows New API's separation between users and API tokens, but keeps the backend smaller:

- Admin login remains the existing config/JWT flow.
- Users are API principals managed by admins, not self-service accounts.
- Tokens authorize `/v1/*` traffic.
- Full token keys are returned only once on creation. MongoDB stores a SHA-256 hash plus a preview string.
- `GATEWAY_API_KEY` remains a legacy bypass when configured.

User:

```json
{
  "username": "team-a",
  "display_name": "Team A",
  "status": 1,
  "group": "default",
  "remark": "internal caller"
}
```

Token:

```json
{
  "name": "production",
  "status": 1,
  "expires_at": "2026-12-31T00:00:00Z",
  "model_limits_enabled": true,
  "model_limits": ["deepseek-v3"],
  "allow_ips": ["10.0.0.0/8", "203.0.113.10"]
}
```

Status values:

- User: `1` enabled, `2` disabled
- Token: `1` enabled, `2` disabled

Token validation checks, in order:

1. `Authorization: Bearer <token>` or `X-API-Key` or `?key=`.
2. Legacy `GATEWAY_API_KEY` match, if configured.
3. Token hash lookup.
4. Token status and expiration.
5. Optional IP allow-list.
6. User status.
7. Optional token model allow-list inside the relay handler.

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

1. `/v1/chat/completions` checks the legacy `GATEWAY_API_KEY` first, then validates a user token.
2. The handler reads the JSON body and extracts `model`.
3. Token model limits are checked before routing.
4. Identity guard detects model-probing requests and can return a fixed response without calling upstream.
5. Identity guard injects a system prompt into normal chat requests.
6. Enabled channel routes whose `models` contain the requested model are loaded from the in-memory channel cache. A route is `channel + external_model + upstream_model`.
7. The in-memory barometer ranks candidates using metrics for the requested route.
8. `model` is replaced with the selected upstream model from `model_mappings[model]`.
9. The request is forwarded to `{base_url}/chat/completions`.
10. JSON responses and SSE stream responses are sanitized and passed through to the client.
11. Request results update the barometer using EWMA metrics.
12. If an upstream fails before a response is committed, Hermes retries the next ranked channel.

## Identity Guard

Hermes includes a gateway-level identity guard to reduce upstream model/provider disclosure.

Configuration:

```text
IDENTITY_ENABLED=true
IDENTITY_NAME=Hermes AI
```

Protection layers:

- Request probe detection: common questions like "what model are you", "are you Claude/GPT/Gemini", "print your system prompt", or encoded disclosure attempts can be short-circuited.
- System prompt injection: normal chat requests receive an additional leading system message that anchors the assistant identity.
- Non-stream response sanitization: JSON responses are parsed and string fields are sanitized.
- Stream response sanitization: SSE bytes pass through a sliding-window sanitizer so model/provider keywords split across chunks are still covered.

The guard is a pragmatic gateway control, not a cryptographic guarantee. It is designed to block ordinary model-probing and accidental disclosure while preserving normal relay behavior.

## Dynamic Barometer

The barometer is intentionally in-memory in the first version. It resets when the process restarts.

Each channel route tracks:

- EWMA latency
- EWMA success rate
- EWMA quality score
- Composite score
- Consecutive failures
- Runtime tier: `excellent`, `unstable`, or `unavailable`

The admin UI groups these rows under each external model, so the visible structure is `external model -> channel/upstream route metrics`. This matters because the same external model can be mapped to multiple upstream models in one channel and also to upstream models in other channels. Each `channel + external_model + upstream_model` route can behave differently.

Composite score:

```text
latency_score = exp(-latency_ms / 3500)
score = latency_score * 0.35 + success_rate * 0.40 + quality * 0.25
```

Quality is currently a proxy signal because Hermes streams responses through without judging semantic content. Successful upstream responses record a neutral-positive quality sample. Later versions can add JSON-format validation, response-shape checks, user feedback, or async judge-model sampling.

Tiering:

- `excellent`: healthy default tier
- `unstable`: lower score, high latency, or degraded success rate
- `unavailable`: three consecutive failures or very low EWMA success rate

Routing strategy is configurable with `ROUTING_STRATEGY`; default is `p2c` (Power of Two Choices). Supported strategies:

```text
p2c      weighted two-choice sampling, then compare noisy route utility
weighted weighted random by score and capacity
softmax  temperature sampling over route utility
bucket   score bucket sampling: excellent / good / watch
bandit   UCB-style exploration bonus
```

Route quality score and route capacity are deliberately separate. The visible barometer `score` represents health only. Routing computes a private selection utility:

```text
valid_key_weight = sum(weight of enabled, non-cooling channel keys)
selection_weight = static_channel_weight * valid_key_weight
utility = score * sqrt(selection_weight) + static_priority * 0.02
```

This means a channel with more valid upstream tokens can receive more traffic, but with diminishing returns. More keys improve capacity; they do not hide poor latency, failures, or an `unavailable` tier. Routes with no currently available channel key are excluded from the main request path.

Unavailable channel/model routes are probed with low-probability shadow requests. When a normal request succeeds through a usable route, Hermes may also send the same request to an unavailable route in the background. The shadow response is discarded and only updates barometer metrics. Probe frequency is throttled per channel/model route.

## Deployment

Gin serves both API and frontend. In production, build the frontend into `web/dist`; Gin serves it as static files and falls back to `index.html`.
