# API Reference

go-orca exposes a JSON REST API served by Gin. All endpoints return `application/json`. SSE endpoints return `text/event-stream`.

## Authentication

go-orca does not include built-in authentication. Secure the API with a reverse proxy (nginx, Caddy, etc.) or network-level controls.

## Request Headers

| Header | Required | Description |
|---|---|---|
| `X-Tenant-ID` | No | Tenant to operate in. Falls back to the server default. |
| `X-Scope-ID` | No | Scope within the tenant. Falls back to the server default scope. |
| `Content-Type: application/json` | For POST/PATCH | Required on requests with a body. |

---

## Schemas

### WorkflowStatus

| Value | Description |
|---|---|
| `pending` | Created, not yet picked up by the scheduler |
| `running` | Actively executing in the engine |
| `paused` | Suspended mid-pipeline; resume with `POST /workflows/{id}/resume` |
| `completed` | All phases finished successfully |
| `failed` | An unrecoverable error occurred |
| `cancelled` | Cancelled via `POST /workflows/{id}/cancel` |

### WorkflowMode

| Value | Description |
|---|---|
| `software` | Software development task |
| `content` | Content creation task |
| `docs` | Documentation task |
| `research` | Research or analysis task |
| `ops` | Operations or infrastructure task |
| `mixed` | Mixed or general-purpose task |

The Director selects a mode automatically from the request when not specified.

### TaskStatus

`pending` | `ready` | `running` | `completed` | `failed` | `skipped` | `blocked`

### PersonaKind

`director` | `project_manager` | `architect` | `implementer` | `qa` | `finalizer` | `refiner`

### ArtifactKind

| Value | Description |
|---|---|
| `code` | Source code |
| `document` | General document |
| `diagram` | Architecture or flow diagram |
| `markdown` | Markdown content |
| `config` | Configuration file (YAML, JSON, TOML, etc.) |
| `report` | Analysis or research report |
| `blog_post` | Publication-ready blog post |
| `bundle_ref` | Reference to an exported artifact bundle |

### ScopeKind

`global` | `org` | `team`

### EventType

| Value | Payload shape |
|---|---|
| `state_transition` | `{ "from": WorkflowStatus, "to": WorkflowStatus }` |
| `persona_started` | `{ "persona": PersonaKind, "provider_name": string, "model_name": string }` |
| `persona_completed` | `{ "persona": PersonaKind, "duration_ms": integer, "summary": string, "blocking_issues": string[] }` |
| `persona_failed` | `{ "persona": PersonaKind, "error": string }` |
| `task_started` | `{ "task_id": uuid, "title": string }` |
| `task_completed` | `{ "task_id": uuid, "title": string }` |

---

## Health Probes

### GET /healthz

Liveness probe. Returns immediately without hitting the database. Use as a Kubernetes `livenessProbe`.

**Response 200**
```json
{"status": "ok"}
```

---

### GET /readyz

Readiness probe. Performs a `Store.Ping` to verify database connectivity. Use as a Kubernetes `readinessProbe`.

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

List workflows for the current tenant/scope, ordered newest first.

**Query parameters**

| Parameter | Type | Default | Description |
|---|---|---|---|
| `limit` | integer | 20 | Maximum number of results (max 200) |
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

**Examples**

Software workflow:
```json
{
  "request": "Build a REST API for a todo list in Go",
  "mode": "software",
  "provider": "openai",
  "model": "gpt-4o"
}
```

Content workflow:
```json
{
  "request": "Write a technical blog post about Go generics",
  "mode": "content"
}
```

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

**Response 400** — missing or invalid request body

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
  "constitution": { "vision": "...", "goals": [...], "constraints": [...], "audience": "...", "acceptance_criteria": [...], "out_of_scope": [...] },
  "requirements": { "functional": [...], "non_functional": [...], "dependencies": [...] },
  "design": { "overview": "...", "components": [...], "decisions": [...], "tech_stack": [...], "delivery_target": "..." },
  "tasks": [ { "id": "...", "title": "...", "status": "completed", "depends_on": [], "assigned_to": "implementer", ... } ],
  "artifacts": [ { "id": "...", "kind": "code", "name": "main.go", "content": "...", "created_by": "implementer", ... } ],
  "finalization": { "action": "markdown-export", "summary": "...", "links": [], "suggestions": [...] },
  "summaries": { "director": "...", "project_manager": "..." },
  "blocking_issues": [],
  "all_suggestions": ["Consider adding rate limiting", "..."],
  "error_message": null,
  "provider_name": "openai",
  "model_name": "gpt-4o",
  "created_at": "...",
  "updated_at": "...",
  "started_at": "...",
  "completed_at": "..."
}
```

**WorkflowState fields**

| Field | Type | Description |
|---|---|---|
| `id` | uuid | Workflow identifier |
| `tenant_id` | uuid | Owning tenant |
| `scope_id` | uuid | Scope within the tenant |
| `status` | WorkflowStatus | Current lifecycle state |
| `mode` | WorkflowMode | Type of work being performed |
| `title` | string | Human-readable title set by the Director |
| `request` | string | Original natural-language request |
| `constitution` | object\|null | Project Manager's foundational document |
| `requirements` | object\|null | Structured requirements from the Project Manager |
| `design` | object\|null | Architect's design artifact |
| `tasks` | array | Task graph produced by the Architect |
| `artifacts` | array | All produced outputs across all phases |
| `finalization` | object\|null | Finalizer's delivery result |
| `summaries` | object | Per-persona compressed summaries keyed by PersonaKind |
| `blocking_issues` | string[] | Blocking issues raised by QA that caused an Implementer re-run |
| `all_suggestions` | string[] | Non-blocking suggestions accumulated across all phases |
| `error_message` | string\|null | Error detail when `status` is `failed` |
| `provider_name` | string | LLM provider selected by the Director |
| `model_name` | string | Model selected by the Director |
| `created_at` | datetime | — |
| `updated_at` | datetime | — |
| `started_at` | datetime\|null | — |
| `completed_at` | datetime\|null | — |

**Response 404** — workflow not found

---

### GET /workflows/:id/events

Return all journal events for a workflow in insertion order. Events are immutable once written.

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
    "id": "uuid",
    "workflow_id": "uuid",
    "type": "persona_started",
    "persona": "director",
    "payload": { "persona": "director", "provider_name": "openai", "model_name": "gpt-4o" },
    "created_at": "2024-01-01T00:00:02Z"
  }
]
```

**Response 404** — workflow not found

---

### GET /workflows/:id/stream

Server-Sent Events stream of workflow events. Polls the event journal every second and pushes new events as they arrive. Sends periodic keepalive comments (`": keepalive"`) to prevent proxy timeouts.

The stream closes automatically when the workflow reaches a terminal state (`completed`, `failed`, `cancelled`) or when the timeout elapses.

> **Reverse proxy configuration:** Set `proxy_buffering off` (nginx) or the equivalent directive to receive events without buffering delay.

**Query parameters**

| Parameter | Type | Default | Description |
|---|---|---|---|
| `timeout` | integer (seconds) | 300 | Maximum stream duration |

**Response** — `text/event-stream`

```
data: {"type":"state_transition","payload":{"from":"pending","to":"running"}}

data: {"type":"persona_started","persona":"director","payload":{"provider_name":"openai","model_name":"gpt-4o"}}

: keepalive

data: {"type":"persona_completed","persona":"director","payload":{"duration_ms":3200,"summary":"..."}}
```

**Response 404** — workflow not found

---

### POST /workflows/:id/cancel

Cancel a workflow. Only `pending` and `running` workflows can be cancelled. Terminal workflows (`completed`, `failed`, `cancelled`) return `409 Conflict`.

**Response 200**
```json
{"status": "cancelled"}
```

**Response 404** — workflow not found

**Response 409** — workflow is already in a terminal state

---

### POST /workflows/:id/resume

Resume a `paused` workflow by re-enqueuing it in the scheduler. The engine reloads the saved state and continues from the last completed phase. Only workflows in `paused` status can be resumed.

**Response 200**
```json
{"status": "running"}
```

**Response 404** — workflow not found

**Response 409** — workflow is not in `paused` status
```json
{"error": "workflow is not paused"}
```

---

## Providers

### GET /providers

List all registered LLM providers and their current status.

**Response 200**
```json
[
  { "name": "openai", "default_model": "gpt-4o", "enabled": true },
  { "name": "ollama", "default_model": "llama3", "enabled": false },
  { "name": "copilot", "default_model": "gpt-4o", "enabled": false }
]
```

---

### POST /providers/:name/test

Test connectivity to a provider. Sends a minimal prompt and checks for a valid response. Valid provider names are `openai`, `ollama`, and `copilot`.

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

**Response 404** — scope not found

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

Create a tenant. The slug must be unique across all tenants.

**Request body**
```json
{ "slug": "acme-corp", "name": "Acme Corporation" }
```

| Field | Required | Description |
|---|---|---|
| `slug` | Yes | URL-safe identifier. Must be unique. |
| `name` | Yes | Display name. |

**Response 201** — created `Tenant` object

**Response 400** — missing or invalid fields

**Response 409** — a tenant with this slug already exists
```json
{"error": "tenant slug already in use"}
```

---

### GET /tenants/:id

Get a single tenant.

**Response 200** — `Tenant` object

**Response 404** — tenant not found

---

### PATCH /tenants/:id

Update tenant name or slug. All fields are optional; only provided fields are updated.

**Request body**
```json
{ "slug": "acme-corp-updated", "name": "Acme Corp (Updated)" }
```

**Response 200** — updated `Tenant` object

**Response 400** — invalid fields

**Response 404** — tenant not found

**Response 409** — the new slug is already in use by another tenant

---

### DELETE /tenants/:id

Delete a tenant and all its scopes. This operation is irreversible.

**Response 204** — no content

**Response 404** — tenant not found

---

### POST /tenants/:id/scopes

Create a scope within a tenant. The scope kind must be enabled by the server's `scoping` configuration.

- `global` scopes have no parent.
- `org` scopes may optionally reference a `global` parent.
- `team` scopes require an `org` parent when `require_team_parent_org` is enabled.

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
| `slug` | Yes | URL-safe identifier. Must be unique within the tenant. |
| `parent_scope_id` | No | Required for `team` scopes when `require_team_parent_org` is enabled; optional for `org` scopes. |

**Response 201** — created `Scope` object

**Response 400** — missing or invalid fields

**Response 404** — tenant not found

**Response 409** — a scope with this slug already exists in the tenant

---

### GET /tenants/:id/scopes

List all scopes for a tenant.

**Response 200**
```json
[
  { "id": "uuid", "tenant_id": "uuid", "kind": "global", "name": "Global", "slug": "global", "parent_scope_id": null, "created_at": "...", "updated_at": "..." },
  { "id": "uuid", "tenant_id": "uuid", "kind": "org", "name": "Engineering", "slug": "engineering", "parent_scope_id": "global-scope-uuid", "created_at": "...", "updated_at": "..." }
]
```

**Response 404** — tenant not found

---

### PATCH /tenants/:id/scopes/:scopeId

Update a scope's name or slug. All fields are optional; only provided fields are updated.

**Request body**
```json
{ "name": "Platform Engineering", "slug": "platform" }
```

**Response 200** — updated `Scope` object

**Response 400** — invalid fields

**Response 404** — tenant or scope not found

**Response 409** — the new slug is already in use within this tenant

---

### DELETE /tenants/:id/scopes/:scopeId

Delete a scope. This operation is irreversible.

**Response 204** — no content

**Response 404** — tenant or scope not found

---

## Customizations

### GET /customizations/resolve

Return the resolved customization snapshot for the current scope. Shows which skills, agent personas, and prompt overlays are active, along with their source and precedence (lower value = higher priority).

**Response 200**
```json
{
  "scope_id": "uuid",
  "skills": [
    { "name": "skill", "source": "global", "precedence": 30, "path": "./customizations/global/SKILL.md" }
  ],
  "agents": [
    { "name": "senior-dev", "source": "team-engineering", "precedence": 10, "path": "./customizations/team-engineering/senior-dev.agent.md" }
  ],
  "prompts": [
    { "name": "safety", "source": "global", "precedence": 30, "path": "./customizations/global/safety.prompt.md" }
  ]
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
