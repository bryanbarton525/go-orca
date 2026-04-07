# API Reference

go-orca exposes a JSON REST API served by Gin. All endpoints return `application/json`. SSE endpoints return `text/event-stream`.

## Request Headers

| Header | Required | Description |
|---|---|---|
| `X-Tenant-ID` | No | Tenant to operate in. Falls back to the server default. |
| `X-Scope-ID` | No | Scope within the tenant. Falls back to the server default scope. |
| `Content-Type: application/json` | For POST/PATCH | Required on requests with a body. |

## Health Probes

### GET /healthz

Liveness probe. Returns immediately without hitting the database.

**Response 200**
```json
{"status": "ok"}
```

---

### GET /readyz

Readiness probe. Performs a `Store.Ping` to verify database connectivity.

**Response 200** — database reachable
```json
{"status": "ready"}
```

**Response 503** — database unreachable
```json
{"status": "unavailable", "error": "<message>"}
```

---

## Workflows

### GET /workflows

List workflows for the current tenant/scope.

**Query parameters**

| Parameter | Type | Default | Description |
|---|---|---|---|
| `limit` | integer | 20 | Maximum number of results |
| `offset` | integer | 0 | Pagination offset |

**Response 200**
```json
[
  {
    "id": "uuid",
    "tenant_id": "uuid",
    "scope_id": "uuid",
    "status": "completed",
    "mode": "software",
    "title": "REST API for todo list",
    "request": "Build a REST API for a todo list in Go",
    "provider_name": "openai",
    "model_name": "gpt-4o",
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:05:00Z",
    "started_at": "2024-01-01T00:00:01Z",
    "completed_at": "2024-01-01T00:05:00Z"
  }
]
```

---

### POST /workflows

Create and enqueue a new workflow.

**Request body**

```json
{
  "request": "Build a REST API for a todo list in Go",
  "title": "Todo API",
  "mode": "software",
  "provider": "openai",
  "model": "gpt-4o"
}
```

| Field | Required | Description |
|---|---|---|
| `request` | Yes | The natural-language task description |
| `title` | No | Human-readable title; Director will set one if omitted |
| `mode` | No | `software` \| `content` \| `docs` \| `research` \| `ops` \| `mixed`; Director selects if omitted |
| `provider` | No | Override LLM provider for this run |
| `model` | No | Override model for this run |

**Response 202**

Returns the newly created `WorkflowState` (status will be `pending`).

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "pending",
  "request": "Build a REST API for a todo list in Go",
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

---

### GET /workflows/:id

Retrieve a single workflow with all persisted state.

**Response 200** — full `WorkflowState` object

```json
{
  "id": "uuid",
  "tenant_id": "uuid",
  "scope_id": "uuid",
  "status": "completed",
  "mode": "software",
  "title": "Todo API",
  "request": "...",
  "constitution": { "vision": "...", "goals": [...], ... },
  "requirements": { "functional": [...], "non_functional": [...], ... },
  "design": { "overview": "...", "components": [...], ... },
  "tasks": [ { "id": "...", "title": "...", "status": "completed", ... } ],
  "artifacts": [ { "id": "...", "kind": "code", "name": "main.go", "content": "...", ... } ],
  "finalization": { "action": "markdown-export", "summary": "...", ... },
  "summaries": { "director": "...", "project_manager": "...", ... },
  "provider_name": "openai",
  "model_name": "gpt-4o",
  "created_at": "...",
  "updated_at": "...",
  "started_at": "...",
  "completed_at": "..."
}
```

**Response 404** — workflow not found

---

### GET /workflows/:id/events

Return all journal events for a workflow in insertion order.

**Response 200**
```json
[
  {
    "id": "uuid",
    "workflow_id": "uuid",
    "tenant_id": "uuid",
    "scope_id": "uuid",
    "type": "state_transition",
    "persona": "",
    "payload": { "from": "pending", "to": "running" },
    "created_at": "2024-01-01T00:00:01Z"
  },
  {
    "type": "persona_started",
    "persona": "director",
    "payload": { "persona": "director", "provider_name": "openai", "model_name": "gpt-4o" }
  }
]
```

---

### GET /workflows/:id/stream

Server-Sent Events stream of workflow events. Polls the event journal every second and pushes new events as they arrive. Sends periodic keepalive comments (`": keepalive"`) to prevent proxy timeouts.

The stream closes automatically when the workflow reaches a terminal state (`completed`, `failed`, `cancelled`) or when the timeout elapses.

**Query parameters**

| Parameter | Type | Default | Description |
|---|---|---|---|
| `timeout` | integer (seconds) | 300 | Maximum stream duration |

**Response** — `text/event-stream`

```
data: {"type":"state_transition","payload":{"from":"pending","to":"running"}}

data: {"type":"persona_started","persona":"director","payload":{...}}

: keepalive

data: {"type":"persona_completed","persona":"director","payload":{"duration_ms":3200,...}}
```

---

### POST /workflows/:id/cancel

Cancel a workflow. Only `pending` and `running` workflows can be cancelled.

**Response 200**
```json
{"status": "cancelled"}
```

**Response 409** — workflow is already in a terminal state

---

### POST /workflows/:id/resume

Resume a `paused` workflow by re-enqueuing it in the scheduler.

**Response 200**
```json
{"status": "running"}
```

**Response 409** — workflow is not in `paused` status

---

## Providers

### GET /providers

List all registered LLM providers.

**Response 200**
```json
[
  { "name": "openai", "default_model": "gpt-4o", "enabled": true },
  { "name": "ollama", "default_model": "llama3", "enabled": false }
]
```

---

### POST /providers/:name/test

Test connectivity to a provider. Sends a minimal prompt and checks for a valid response.

**Response 200** — provider reachable
```json
{"name": "openai", "ok": true, "latency_ms": 412}
```

**Response 502** — provider unreachable or returned an error
```json
{"name": "openai", "ok": false, "error": "connection refused"}
```

---

## Scopes

### GET /scopes/:id/effective-config

Return the effective (merged) configuration for a scope. Walks the scope chain from `global` → `org` → `team`, merging each level.

**Response 200**
```json
{
  "scope_id": "uuid",
  "scope_kind": "team",
  "resolved_chain": ["global-scope-id", "org-scope-id", "team-scope-id"],
  "effective": { ... }
}
```

---

## Tenants

### GET /tenants

List all tenants.

**Response 200**
```json
[
  { "id": "uuid", "slug": "default", "name": "Default", "created_at": "...", "updated_at": "..." }
]
```

---

### POST /tenants

Create a tenant.

**Request body**
```json
{ "slug": "acme", "name": "Acme Corp" }
```

**Response 201** — created `Tenant` object

---

### GET /tenants/:id

Get a single tenant.

**Response 200** — `Tenant` object

---

### PATCH /tenants/:id

Update tenant name or slug.

**Request body** (all fields optional)
```json
{ "slug": "acme-corp", "name": "Acme Corporation" }
```

**Response 200** — updated `Tenant` object

---

### DELETE /tenants/:id

Delete a tenant and all its scopes.

**Response 204** — no content

---

### POST /tenants/:id/scopes

Create a scope within a tenant.

**Request body**
```json
{
  "kind": "org",
  "name": "Engineering",
  "slug": "engineering",
  "parent_scope_id": "global-scope-uuid"
}
```

| Field | Required | Description |
|---|---|---|
| `kind` | Yes | `global` \| `org` \| `team` |
| `name` | Yes | Display name |
| `slug` | Yes | URL-safe identifier |
| `parent_scope_id` | No | Required for `team` scopes; optional for `org` |

**Response 201** — created `Scope` object

---

### GET /tenants/:id/scopes

List all scopes for a tenant.

**Response 200**
```json
[
  { "id": "uuid", "tenant_id": "uuid", "kind": "global", "name": "Global", "slug": "global", ... },
  { "id": "uuid", "tenant_id": "uuid", "kind": "org", "name": "Engineering", "slug": "engineering", "parent_scope_id": "...", ... }
]
```

---

### PATCH /tenants/:id/scopes/:scopeId

Update a scope's name or slug.

**Request body** (all fields optional)
```json
{ "name": "Platform Engineering", "slug": "platform" }
```

**Response 200** — updated `Scope` object

---

### DELETE /tenants/:id/scopes/:scopeId

Delete a scope.

**Response 204** — no content

---

## Customizations

### GET /customizations/resolve

Return the resolved customization snapshot for the current scope. Shows which skills, agent personas, and prompt overlays are active.

**Response 200**
```json
{
  "scope_id": "uuid",
  "skills": [
    { "name": "skill", "source": "local-skills", "precedence": 10, "path": "./customizations/SKILL.md" }
  ],
  "agents": [
    { "name": "senior-dev", "source": "repo-agents", "precedence": 5, "path": "./.agents/senior-dev.agent.md" }
  ],
  "prompts": []
}
```

---

## Error Responses

All error responses use this shape:

```json
{ "error": "<human-readable message>" }
```

| Status | Meaning |
|---|---|
| 400 | Invalid request body or missing required field |
| 404 | Resource not found |
| 409 | Conflict (e.g. workflow already terminal, slug already in use) |
| 500 | Internal server error |
| 502 | Upstream provider error (provider test endpoint) |
| 503 | Service unavailable (readiness probe) |
