# go-orca

<p align="center">
  <img src="/docs/images/go-orca.png" alt="go-orca logo" width="200" />
</p>

<p align="center">
  <strong>Multi-tenant AI orchestration engine</strong><br/>
  Drive structured multi-phase LLM pipelines from a single HTTP API — with built-in QA remediation, scoped customization, and automatic self-improvement after every run.
</p>

<p align="center">
  <a href="https://bryanbarton525.github.io/go-orca/"><img src="https://img.shields.io/badge/API%20Docs-Swagger%20UI-85EA2D?logo=swagger" alt="API Docs"></a>
</p>

---

go-orca is a self-hosted backend service that runs structured AI workflows across isolated tenants and scope hierarchies. Submit a natural-language request; go-orca routes it through a six-persona pipeline — Director → Project Manager → Architect → Implementer → QA → Finalizer — producing typed artifacts that can be delivered as a GitHub PR, a markdown export, a blog draft, a doc draft, a webhook payload, or an artifact bundle.

Every workflow is isolated by tenant and scope. Per-scope customizations (skills, prompt overlays, agent personas) let different teams within the same deployment drive different behaviour without touching shared configuration. The scope hierarchy (global → org → team) means tenant-level defaults cascade to narrower scopes automatically.

After every workflow the Finalizer runs an inline **Refiner** retrospective — automatically, with no extra configuration — and emits `refiner.suggestion` SSE events with structured improvement proposals. The engine enforces strict role contracts: only the Architect may produce tasks, only the Implementer may produce artifacts, QA is validation-only. Violations are discarded and recorded rather than silently corrupting state.

## Features

- **Multi-tenant isolation** — every workflow, event, and artifact is scoped to a tenant + scope; read endpoints enforce ownership so tenants cannot access each other's data
- **Scope hierarchy** — global → org → team customization chain; narrower scopes inherit and override broader ones
- **Automatic self-improvement** — inline Refiner retrospective runs after every workflow, producing structured proposals with component name, problem, proposed fix, priority, and health score (0–100)
- **Cross-workflow pattern detection** — standalone async Refiner persona analyses historical journal events across many runs to surface recurring issues single-run retrospectives cannot see
- **Architect-led QA remediation** — when QA raises blocking issues, the Architect re-plans with targeted new tasks; Implementer executes them; QA re-validates; repeats up to `MaxQARetries` times
- **Role contract enforcement** — engine discards and warns on any output that violates persona ownership rules (Artifacts from non-Implementer, Tasks from non-Architect, etc.)
- **Seven delivery actions** — GitHub PR, direct repo commit, artifact bundle, markdown export, blog draft, doc draft, webhook dispatch; caller-provided `delivery.action` overrides LLM choice
- **Live execution progress** — `GET /workflows/:id` exposes `execution.current_persona`, `active_task_id`, `qa_cycle`, and `remediation_attempt` for in-flight visibility without SSE
- **SSE streaming** — real-time `text/event-stream` feed with dotted event type names (`persona.started`, `state.transition`, `refiner.suggestion`, etc.)
- **Pause and resume** — workflows can be paused mid-pipeline and resumed via the API
- **Four LLM backends** — OpenAI, Anthropic Claude, Ollama (local), GitHub Copilot
- **Built-in tools + MCP** — `http_get`, `read_file`, `write_file`, plus remote tools via Model Context Protocol
- **SQLite or PostgreSQL** — swap backends with a single config line; auto-migration included
- **Structured logging** — JSON or console output via zap

## Self-Improvement

go-orca is designed to improve its own behaviour over time without manual intervention.

**Inline Refiner (every workflow)**

After the Finalizer delivers its output it automatically runs a Refiner retrospective — synchronously, on every completed workflow. The Refiner returns structured improvement proposals:

| Field | Description |
|---|---|
| `component_type` | `agent`, `skill`, `prompt`, `persona`, `workflow`, or `provider` |
| `component_name` | Exact name of the file or component to change |
| `problem` | What went wrong or could be better |
| `proposed_fix` | Concrete suggested change |
| `priority` | `high`, `medium`, or `low` |
| `health_score` | 0–100 overall run quality score |

Suggestions are stored on the workflow state (`all_suggestions`) and emitted as `refiner.suggestion` SSE events. A Refiner failure never breaks a workflow — errors are intentionally swallowed.

**Standalone async Refiner (cross-workflow)**

A separate `refiner` persona can be run as a background loop over historical workflow journal events. Because it sees many runs at once it can identify recurring patterns — systemic prompt weaknesses, repeatedly failing skills, consistent QA regressions — that single-run retrospectives cannot surface.

## Quick Start

### 1. Build

```bash
go build -o go-orca-api ./cmd/go-orca-api
```

### 2. Configure

Copy the example config and set at least one provider:

```bash
cp go-orca.yaml my-config.yaml
# Edit my-config.yaml: enable a provider, set API key
```

Or use environment variables:

```bash
export GOORCA_PROVIDERS_OPENAI_ENABLED=true
export GOORCA_PROVIDERS_OPENAI_API_KEY=sk-...
```

### 3. Run

```bash
./go-orca-api -config my-config.yaml
```

The server starts on `http://0.0.0.0:8080` by default.

### 4. Submit a workflow

```bash
curl -s -X POST http://localhost:8080/workflows \
  -H 'Content-Type: application/json' \
  -H 'X-Tenant-ID: acme' \
  -H 'X-Scope-ID: eng-team' \
  -d '{"request": "Build a REST API for a todo list in Go"}' | jq .
```

### 5. Stream events

```bash
curl -N http://localhost:8080/workflows/<id>/stream \
  -H 'X-Tenant-ID: acme'
```

## Documentation

| Document | Description |
|---|---|
| [Architecture](docs/architecture.md) | System overview, component map, trust boundaries, data flow |
| [Workflow Engine](docs/workflow-engine.md) | Persona pipeline, QA remediation loop, role enforcement, execution progress, pause/resume |
| [API Reference](docs/api.md) | All HTTP endpoints, request/response schemas, headers, event types |
| [Configuration](docs/configuration.md) | Every config key, default, env var override |
| [Providers](docs/providers.md) | OpenAI, Anthropic, Ollama, GitHub Copilot — setup and config |
| [Customization](docs/customization.md) | Skills, agent personas, prompt overlays, scope resolution chain |
| [Finalizer Actions](docs/finalizer-actions.md) | Delivery actions: GitHub PR, commit, bundle, export, blog draft, doc draft, webhook |
| [Tools](docs/tools.md) | Built-in tools and the MCP remote tool protocol |
| [Storage](docs/storage.md) | SQLite vs PostgreSQL, Store interface, migrations |
| [Deployment](docs/deployment.md) | Binary setup, Docker, reverse proxy, shutdown behaviour |

## License

MIT

