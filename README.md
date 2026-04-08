# go-orca

<p align="center">
  <img src="/docs/images/go-orca.png" alt="go-orca logo" width="200" />
</p>

<p align="center">
  A self-hosted AI workflow orchestration API server that improves itself.<br/>
  Drive multi-phase LLM pipelines from a single HTTP API — and get structured improvement proposals after every run.
</p>

<p align="center">
  <a href="https://bryanbarton525.github.io/go-orca/"><img src="https://img.shields.io/badge/API%20Docs-Swagger%20UI-85EA2D?logo=swagger" alt="API Docs"></a>
</p>

---

go-orca is a backend service that orchestrates multi-agent AI workflows through a structured persona pipeline. You submit a natural-language request; go-orca drives it through Director → Project Manager → Architect → Implementer → QA → Finalizer, producing structured artifacts that can be delivered as a GitHub PR, a markdown export, a webhook payload, or an artifact bundle.

After every workflow the Finalizer runs an inline **Refiner** retrospective — automatically, with no extra configuration. The Refiner receives the full workflow history and produces structured improvement proposals (component, problem, proposed fix, priority, health score) that tell you exactly what to tune and where. A standalone async Refiner persona can also analyse patterns across many historical workflows at once.

## Features

- **Automatic self-improvement** — after every workflow the Finalizer runs an inline Refiner retrospective that produces structured improvement proposals: component name, problem, proposed fix, priority, and an overall health score (0–100)
- **Cross-workflow pattern detection** — a standalone async Refiner persona analyses historical workflow events across many runs to identify recurring issues that single-run retrospectives cannot see
- **Structured persona pipeline** — six specialist roles, each with a distinct purpose and typed output
- **QA retry loop** — blocking issues automatically re-invoke the Implementer before re-running QA
- **Pause and resume** — workflows can be paused mid-pipeline and resumed via the API
- **Three LLM backends** — OpenAI, Ollama (local), and GitHub Copilot
- **Multi-tenancy and scoping** — tenant + scope hierarchy (global → org → team) with per-scope customizations
- **Customization system** — inject skills, agent personas, and prompt overlays from the filesystem or a repo
- **Six delivery actions** — GitHub PR, direct commit, artifact bundle, markdown export, blog draft, webhook
- **Built-in tools + MCP** — `http_get`, `read_file`, `write_file`, plus remote tools via Model Context Protocol
- **SQLite or PostgreSQL** — swap backends with a single config line; auto-migration included
- **SSE streaming** — real-time workflow event stream for live UI integration
- **Structured logging** — JSON or console output via zap

## Self-Improvement

go-orca is designed to improve its own behaviour over time without manual intervention.

**Inline Refiner (every workflow)**

After the Finalizer delivers its output it automatically runs a Refiner retrospective — synchronously, on every completed workflow, with no extra configuration required. The Refiner receives:

- All persona summaries from the run
- Accumulated blocking issues
- The full suggestion history
- All produced artifacts

It returns structured improvement proposals:

| Field | Description |
|---|---|
| `component_type` | `agent`, `skill`, `prompt`, `persona`, `workflow`, or `provider` |
| `component_name` | Exact name of the file or component to change |
| `problem` | What went wrong or could be better |
| `proposed_fix` | Concrete suggested change |
| `example` | Optional illustrative example |
| `priority` | `high`, `medium`, or `low` |

It also returns an `overall_assessment`, a `health_score` (0–100), and a plain-language `summary`. Suggestions are stored on the workflow state (`all_suggestions`) and emitted as `refiner.suggestion` SSE events. A Refiner failure never breaks a workflow — errors are intentionally swallowed so retrospectives cannot interfere with delivery.

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
  -d '{"request": "Build a REST API for a todo list in Go"}' | jq .
```

### 5. Stream events

```bash
curl -N http://localhost:8080/workflows/<id>/stream
```

## Documentation

| Document | Description |
|---|---|
| [Architecture](docs/architecture.md) | System overview, component map, data flow |
| [Workflow Engine](docs/workflow-engine.md) | Persona pipeline, QA retry loop, pause/resume, state machine |
| [API Reference](docs/api.md) | All HTTP endpoints, request/response schemas, headers |
| [Configuration](docs/configuration.md) | Every config key, default, env var override |
| [Providers](docs/providers.md) | OpenAI, Ollama, GitHub Copilot — setup and config |
| [Customization](docs/customization.md) | Skills, agent personas, prompt overlays, source types |
| [Finalizer Actions](docs/finalizer-actions.md) | Delivery actions: GitHub PR, commit, bundle, export, webhook |
| [Tools](docs/tools.md) | Built-in tools and the MCP remote tool protocol |
| [Storage](docs/storage.md) | SQLite vs PostgreSQL, Store interface, migrations |
| [Deployment](docs/deployment.md) | Binary setup, Docker, reverse proxy, graceful shutdown |

## License

MIT
