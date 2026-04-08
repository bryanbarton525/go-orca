# Providers

go-orca supports four LLM backends. All are disabled by default — enable at least one before submitting workflows. The Director persona selects the active provider and model for each workflow run.

## Provider Registry

Providers are registered at startup via `internal/provider/common`. The global registry is a keyed map of `Provider` implementations. The workflow engine resolves the active provider from `WorkflowState.ProviderName` (set by the Director).

## Default Provider Selection

At startup, the first enabled provider in this priority order becomes the default:

1. OpenAI
2. Anthropic
3. Ollama
4. Copilot

The default is used when the Director does not select a provider, or as a fallback.

---

## OpenAI

Package: `internal/provider/openai`

Talks to the OpenAI Chat Completions API (or any OpenAI-compatible endpoint via `base_url`).

### Configuration

```yaml
providers:
  openai:
    enabled: true
    api_key: "sk-..."        # required
    base_url: ""             # optional; defaults to api.openai.com
    default_model: "gpt-4o"
    timeout: "120s"
```

| Key | Required | Description |
|---|---|---|
| `enabled` | Yes | Must be `true` to activate |
| `api_key` | Yes | OpenAI API key; also accepts `GOORCA_PROVIDERS_OPENAI_API_KEY` env var |
| `base_url` | No | Override base URL for OpenAI-compatible APIs (e.g. Azure OpenAI, local proxies) |
| `default_model` | No | Model used when the Director does not specify one. Default: `gpt-4o` |
| `timeout` | No | Per-request timeout. Default: `120s` |

### Compatible Models

Any model available via the Chat Completions API: `gpt-4o`, `gpt-4-turbo`, `gpt-3.5-turbo`, `o1`, etc.

### OpenAI-Compatible Endpoints

Set `base_url` to use a compatible server:

```yaml
providers:
  openai:
    enabled: true
    api_key: "ollama"
    base_url: "http://localhost:11434/v1"  # Ollama OpenAI-compat endpoint
    default_model: "llama3"
```

---

## Ollama

Package: `internal/provider/ollama`

Talks to a locally-running [Ollama](https://ollama.ai) server. No API key required.

### Configuration

```yaml
providers:
  ollama:
    enabled: true
    host: "http://localhost:11434"
    default_model: "llama3"
    timeout: "120s"
```

| Key | Required | Description |
|---|---|---|
| `enabled` | Yes | Must be `true` to activate |
| `host` | No | Ollama server URL. Default: `http://localhost:11434` |
| `default_model` | No | Model tag to use. Default: `llama3` |
| `timeout` | No | Per-request timeout. Default: `120s` |

### Starting Ollama

```bash
# Install
curl -fsSL https://ollama.ai/install.sh | sh

# Pull a model
ollama pull llama3
ollama pull codellama

# Start the server (done automatically after install)
ollama serve
```

### Recommended Models

| Use Case | Model |
|---|---|
| General purpose | `llama3`, `mistral` |
| Code generation | `codellama`, `deepseek-coder` |
| Small / fast | `phi3`, `gemma` |

---

## Anthropic

Package: `internal/provider/anthropic`

Talks to the Anthropic Claude API (Messages API).

### Configuration

```yaml
providers:
  anthropic:
    enabled: true
    api_key: "sk-ant-..."    # required
    base_url: ""             # optional; defaults to api.anthropic.com
    default_model: "claude-opus-4-5"
    max_tokens: 0            # 0 = provider default
    timeout: "120s"
```

| Key | Required | Description |
|---|---|---|
| `enabled` | Yes | Must be `true` to activate |
| `api_key` | Yes | Anthropic API key; also accepts `GOORCA_PROVIDERS_ANTHROPIC_API_KEY` env var |
| `base_url` | No | Override base URL for Anthropic-compatible endpoints |
| `default_model` | No | Model used when the Director does not specify one |
| `max_tokens` | No | Maximum tokens per response. `0` uses the provider default |
| `timeout` | No | Per-request timeout. Default: `120s` |

### Compatible Models

Any model available via the Messages API: `claude-opus-4-5`, `claude-sonnet-4-5`, `claude-haiku-3-5`, etc.

---

## GitHub Copilot

Package: `internal/provider/copilot`

Uses the GitHub Copilot API via a GitHub personal access token (PAT) or the `gh` CLI. Requires a GitHub account with an active Copilot subscription.

### Configuration

```yaml
providers:
  copilot:
    enabled: true
    github_token: "ghp_..."  # required
    cli_path: ""             # optional; path to gh binary
    default_model: "gpt-4o"
```

| Key | Required | Description |
|---|---|---|
| `enabled` | Yes | Must be `true` to activate |
| `github_token` | Yes | GitHub PAT with Copilot access; also accepts `GOORCA_PROVIDERS_COPILOT_GITHUB_TOKEN` |
| `cli_path` | No | Absolute path to the `gh` CLI binary. Empty = resolve from `$PATH` |
| `default_model` | No | Default model. Default: `gpt-4o` |

### Token Requirements

The PAT must have the `copilot` OAuth scope. Create one at: GitHub → Settings → Developer settings → Personal access tokens.

---

## Health Check

Use `POST /providers/:name/test` to verify connectivity:

```bash
curl -s -X POST http://localhost:8080/providers/openai/test | jq .
```

```json
{"name": "openai", "ok": true, "latency_ms": 380}
```

Use `GET /providers` to list all registered providers and their status:

```bash
curl -s http://localhost:8080/providers | jq .
```

---

## Selecting a Provider Per Workflow

Override the provider and model for a single workflow run:

```bash
curl -X POST http://localhost:8080/workflows \
  -H 'Content-Type: application/json' \
  -d '{
    "request": "Write a blog post about Go generics",
    "provider": "ollama",
    "model": "mistral"
  }'
```

If not specified, the Director selects the provider based on the request type and the configured defaults.
