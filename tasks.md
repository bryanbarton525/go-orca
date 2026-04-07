# gorca — Task Tracking

> **Module:** `github.com/gorca/gorca`
> **Go version:** 1.26.1 · darwin/arm64
> Last updated: 2026-04-06

---

## ✅ Completed

### Foundation
- [x] `go.mod` — module `github.com/gorca/gorca`, Go 1.26.1
- [x] All dependencies fetched: gin, viper, openai-go, ollama/api, copilot-sdk/go v0.2.1, uuid, pgx/v5, go-sqlite3, golang-migrate, zap, validator/v10
- [x] `internal/config/config.go` — Viper-based config system (YAML + GORCA_* env vars), all structs, defaults, validation
- [x] `internal/logger/logger.go` — zap structured logger, package-level helpers, Init()

### Providers
- [x] `internal/provider/common/provider.go` — Provider interface, Capability flags, Message/ChatRequest/ChatResponse, registry (Register/Get/All), BaseProvider, streaming helpers
- [x] `internal/provider/openai/openai.go` — OpenAI provider (Chat, Stream, Models, HealthCheck)
- [x] `internal/provider/ollama/ollama.go` — Ollama provider (Chat, Stream, Models, HealthCheck)
- [x] `internal/provider/copilot/copilot.go` — GitHub Copilot provider with correct SDK API (Chat, Stream, Models, HealthCheck, Stop, lazy client init)

### State & Events
- [x] `internal/state/state.go` — canonical workflow state model: WorkflowState, Constitution, Requirements, Design, Task, Artifact, FinalizationResult, HandoffPacket, PersonaOutput, Tenant, Scope, all enums + BlockingIssues/AllSuggestions on HandoffPacket
- [x] `internal/events/journal.go` — append-only event journal: Event, EventType constants, all payload types, Journal interface (doc comment points to storage.EventStore)

### Personas
- [x] `internal/persona/persona.go` — Persona interface + registry (Register/Get/All)
- [x] `internal/persona/base/executor.go` — shared Executor (provider dispatch, system prompt layering, handoff context builder, JSON parser with fence stripping), BuildHandoffContext, ParseJSON
- [x] `internal/persona/base/executor_test.go` — smoke tests: extractJSON (no fence, json fence, plain fence, whitespace, prefix text), ParseJSON (clean, fenced, invalid), BuildHandoffContext (basic fields, summaries, tasks, empty packet)
- [x] `internal/persona/director/director.go` — Director: classifies mode, selects provider/model, decides persona pipeline
- [x] `internal/persona/pm/pm.go` — Project Manager: produces Constitution + Requirements
- [x] `internal/persona/architect/architect.go` — Architect: produces Design + task graph with UUID assignment
- [x] `internal/persona/implementer/implementer.go` — Implementer: per-task artifact production
- [x] `internal/persona/qa/qa.go` — QA: validates artifacts vs constitution/requirements/design, blocking/warning/info severity
- [x] `internal/persona/finalizer/finalizer.go` — Finalizer: delivery action selection + inline Refiner retrospective Phase 2
- [x] `internal/persona/refiner/refiner.go` — Refiner: standalone retrospective persona, systemic improvement proposals

### Workflow Engine
- [x] `internal/workflow/engine/engine.go` — state machine: Director→PM→Architect→Implementer(s)→QA(retry ≤2x)→Finalizer; typed event journal writes; Store interface; buildPacket/applyOutput/transition helpers; ErrPaused, PauseFunc hook, checkPause() between every phase
- [x] `internal/workflow/engine/engine_test.go` — mock store + mock persona smoke tests
- [x] `internal/workflow/scheduler/scheduler.go` — bounded goroutine pool, queue with backpressure, retry-with-delay, graceful drain on shutdown; treats ErrPaused as informational

### Storage
- [x] `internal/storage/store.go` — unified Store interface (WorkflowStore + EventStore + TenantStore + ScopeStore)
- [x] `internal/storage/postgres/postgres.go` — pgx/v5 pool, JSONB columns, upsert, transactional event appends, all Store methods; auto-migrate via golang-migrate/v4 (pgx5:// DSN scheme)
- [x] `internal/storage/sqlite/sqlite.go` — database/sql + go-sqlite3, WAL mode, integer Unix timestamps, embedded DDL, Migrate() helper; NULL columns scanned via sql.NullString
- [x] `internal/storage/sqlite/sqlite_test.go` — Migrate + CRUD round-trip smoke tests
- [x] `internal/storage/migrations/001_initial_schema.up.sql` — tenants, scopes, workflows, workflow_events (Postgres DDL)
- [x] `internal/storage/migrations/001_initial_schema.down.sql` — rollback
- [x] `internal/storage/migrations/002_scope_settings.up.sql` — scope_settings, scope_component_sources, scope_provider_policies, scope_tool_policies, scope_finalizer_policies
- [x] `internal/storage/migrations/002_scope_settings.down.sql` — rollback
- [x] `internal/storage/migrations/003_task_edges.up.sql` — relational task_edges(from_task_id, to_task_id) for graph queries
- [x] `internal/storage/migrations/003_task_edges.down.sql` — rollback

### Services
- [x] `internal/tenant/tenant.go` — tenant CRUD service + EnsureDefault (homelab bootstrap)
- [x] `internal/scope/scope.go` — scope CRUD, hierarchy constraints (global/org/team), ancestor chain ResolveChain

### Extensions
- [x] `internal/customization/customization.go` — GitHub-compatible SKILL.md / *.agent.md / *.prompt.md scanner; multi-source registry with precedence dedup; immutable Snapshot with SkillsContext/AgentsContext/PromptsContext helpers
- [x] `internal/tools/tools.go` — Tool interface, process-wide Global registry, Call/Specs helpers, ToolSpec serialization
- [x] `internal/tools/mcp/mcp.go` — MCP out-of-process adapter: Manifest, ToolDef, MCPTool, Load()
- [x] `internal/tools/builtin/builtin.go` — builtin tools: http_get, read_file, write_file; RegisterAll()
- [x] `internal/finalizer/actions/actions.go` — Action interface + registry; built-in stubs: MarkdownExportAction, ArtifactBundleAction, BlogDraftAction, WebhookAction (stub), GitHubPRAction (stub), RepoCommitAction (stub); Global pre-loaded registry

### API
- [x] `internal/api/middleware/middleware.go` — zap Logger, Recovery, RequestID, TenantFromHeader, ScopeFromHeader
- [x] `internal/api/handlers/handlers.go` — Healthz, Readyz, CreateWorkflow, GetWorkflow, ListWorkflows, GetWorkflowEvents, CancelWorkflow, ResumeWorkflow, ListProviders, TestProvider, GetEffectiveConfig, ResolveCustomizations, CreateTenant, ListTenants, CreateScope, ListScopes, StreamWorkflowEvents (SSE)
- [x] `internal/api/handlers/handlers_test.go` — httptest round-trips: /healthz, POST /workflows, GET /workflows/:id, GET /workflows
- [x] `internal/api/routes/routes.go` — full Gin router with middleware stack; all routes wired including GET /:id/stream
- [x] `cmd/gorca-api/main.go` — application entrypoint: config load, logger init, store open, EnsureDefault, provider registration (OpenAI + Ollama + Copilot), persona registration (all 7), builtin tools registered, engine + scheduler bootstrap, HTTP server with graceful shutdown

### Config
- [x] `gorca.yaml` — example config at repo root with all keys documented

---

## Known Gaps (not in original plan — lower priority)

- `engine.buildPacket()`: `AllSuggestions` loop body is dead code (`_ = sum`); `HandoffPacket.AllSuggestions` is always empty
- `finalizer/actions`: `github-pr`, `repo-commit-only`, `webhook-dispatch` are deliberate stubs with no real implementation
- `scope.validateHierarchy` does not verify parent scope kind (e.g., org's parent must be global)
- `customization.go` builtin source type is a no-op (returns nil, nil)
- `gorca.yaml` `workflow.max_concurrent_workflows` / `handoff_timeout` are not wired into the scheduler/engine
- No `Update`/`Delete` on tenant or scope services
- `toolReg` in `main.go` is populated but not injected into the engine (no engine field for it yet)
- No tests for: ResumeWorkflow handler, SSE stream handler, scope/tenant handlers, customizations handler, provider handlers, persona packages, customization package, scope/tenant service packages, finalizer/actions package, tools packages
