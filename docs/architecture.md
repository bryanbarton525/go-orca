# Architecture

go-orca is a single Go binary that exposes an HTTP API, runs a workflow engine, and talks to one or more LLM providers. This document describes how those components fit together.

## System Overview

```
                          ┌─────────────────────────────────────────────────┐
                          │                  go-orca-api binary              │
                          │                                                   │
   HTTP clients           │   ┌──────────┐     ┌──────────────────────────┐  │
 ──────────────►  :8080   │   │  Gin     │     │     Workflow Engine       │  │
                          │   │  Router  │────►│  Director                 │  │
                          │   └──────────┘     │  Project Manager          │  │
                          │        │           │  Architect                │  │
                          │        │           │  Implementer (per task)   │  │
                          │        ▼           │  QA (retry loop)          │  │
                          │   ┌──────────┐     │  Finalizer                │  │
                          │   │ Store    │     └──────────────────────────┘  │
                          │   │ SQLite / │             │                      │
                          │   │ Postgres │◄────────────┤                      │
                          │   └──────────┘             │                      │
                          │                            ▼                      │
                          │                  ┌──────────────────┐            │
                          │                  │ Provider Registry │            │
                          │                  │  OpenAI / Ollama  │            │
                          │                  │  Copilot          │            │
                          │                  └──────────────────┘            │
                          └─────────────────────────────────────────────────┘
```

## Component Map

| Package | Responsibility |
|---|---|
| `cmd/go-orca-api` | Binary entry point: config loading, bootstrap, HTTP server lifecycle |
| `internal/config` | Viper-backed config loading; `GOORCA_*` env var overrides |
| `internal/api/routes` | Gin router wiring |
| `internal/api/handlers` | HTTP handler functions, SSE streaming |
| `internal/api/middleware` | Recovery, structured logging, request ID, tenant/scope extraction |
| `internal/workflow/engine` | Persona pipeline state machine |
| `internal/workflow/scheduler` | Bounded worker pool; enqueue/retry/shutdown |
| `internal/persona/*` | One package per persona role (director, pm, architect, implementer, qa, finalizer, refiner) |
| `internal/persona` | Global persona registry (`Register` / `Get` / `All`) |
| `internal/state` | Canonical types: `WorkflowState`, `HandoffPacket`, `PersonaOutput`, enums |
| `internal/events` | Event journal types and `NewEvent` constructor |
| `internal/provider/common` | Global provider registry; `Register` / `Get` |
| `internal/provider/openai` | OpenAI provider implementation |
| `internal/provider/ollama` | Ollama provider implementation |
| `internal/provider/copilot` | GitHub Copilot provider implementation |
| `internal/storage` | `Store` interface (the only persistence contract) |
| `internal/storage/sqlite` | SQLite store implementation |
| `internal/storage/postgres` | PostgreSQL store implementation |
| `internal/tenant` | `EnsureDefault`, tenant CRUD helpers |
| `internal/scope` | Scope CRUD, hierarchy validation, `ResolveChain` |
| `internal/customization` | Source scanning, snapshot creation, dedup |
| `internal/finalizer/actions` | Delivery action registry and six built-in actions |
| `internal/tools` | `Tool` interface and `Registry` |
| `internal/tools/builtin` | `http_get`, `read_file`, `write_file` |
| `internal/tools/mcp` | MCP manifest loader; JSON-RPC bridge |
| `internal/logger` | Singleton zap logger initialisation |

## Data Flow: Workflow Lifecycle

```
POST /workflows
       │
       ▼
 CreateWorkflow handler
  1. Resolve tenant + scope from request headers (or defaults)
  2. Construct WorkflowState (status=pending)
  3. Persist via Store.SaveWorkflow
  4. Enqueue workflow ID in Scheduler
  5. Return 202 with workflow JSON
       │
       ▼
 Scheduler.worker
  1. Dequeue workflow ID
  2. Call Engine.Run(workflowID)
       │
       ▼
 Engine.Run
  1. Load WorkflowState from Store
  2. Transition to status=running
  3. Snapshot customizations (once, at workflow start)
  4. Run persona pipeline phases sequentially:
        Director → PM → Architect → Implementer(s) → QA loop → Finalizer
  5. After each persona: merge PersonaOutput into WorkflowState, append event, save
  6. On completion: transition to status=completed
  7. On error:     transition to status=failed
  8. On pause:     transition to status=paused, return ErrPaused
       │
       ▼
 Client polls GET /workflows/:id  or  streams GET /workflows/:id/stream
```

## Event Journal

Every state change and persona transition appends an immutable event to the journal via `Store.AppendEvents`. Events have a `type`, a `persona` field (for persona-scoped events), a typed JSON payload, and a timestamp. Event types include:

- `state_transition` — workflow status change (e.g. `pending → running`)
- `persona_started` — a persona began executing
- `persona_completed` — a persona finished; includes duration and summary
- `persona_failed` — a persona returned an error
- `task_started` / `task_completed` — Implementer per-task events

Clients can retrieve the full event list via `GET /workflows/:id/events`, or subscribe to the live SSE feed via `GET /workflows/:id/stream`.

## Multi-Tenancy and Scope Hierarchy

```
Tenant (e.g. "acme-corp")
  └── Scope: global
        └── Scope: org  (parent = global)
              └── Scope: team  (parent = org)
```

Every API request carries `X-Tenant-ID` and `X-Scope-ID` headers. Missing headers fall back to the server-configured default tenant and default scope. Customization sources are filtered by `scope_slug` so different scopes receive different skill/prompt overlays.

The active `ScopingMode` (global / org / team / hosted) controls which scope kinds are permitted at runtime.

## Request Routing Middleware Stack

Each HTTP request passes through (in order):

1. **Recovery** — catches panics, logs with stack trace, returns `500`
2. **Logger** — structured request/response logging via zap
3. **RequestID** — generates `X-Request-ID` if absent
4. **TenantFromHeader** — reads `X-Tenant-ID`; falls back to default
5. **ScopeFromHeader** — reads `X-Scope-ID`; falls back to default

## Dependency Graph (simplified)

```
main
 ├── config
 ├── logger
 ├── storage (sqlite | postgres)
 ├── tenant / scope
 ├── provider/common ← openai | ollama | copilot
 ├── persona (registry) ← director | pm | architect | implementer | qa | finalizer | refiner
 ├── tools (registry) ← builtin | mcp
 ├── customization (registry)
 ├── workflow/engine ← state, events, persona, customization
 ├── workflow/scheduler ← engine
 └── api/routes ← handlers, middleware, storage, scheduler, customization
```
