# Configuration

go-orca is configured via a YAML file (default: `go-orca.yaml`) with optional environment variable overrides. All config keys map to `GOORCA_<SECTION>_<KEY>` environment variables.

## Loading Order

1. Built-in defaults (applied first)
2. YAML config file — searched in this order unless `-config` is passed:
   - `./go-orca.yaml`
   - `/etc/go-orca/go-orca.yaml`
   - `$HOME/.go-orca/go-orca.yaml`
3. `GOORCA_*` environment variables (highest precedence; override any YAML value)

## CLI Flag

```
go-orca-api -config /path/to/go-orca.yaml
```

## Environment Variable Format

Nested keys use `_` as separator. Examples:

| YAML key | Environment variable |
|---|---|
| `server.port` | `GOORCA_SERVER_PORT` |
| `database.dsn` | `GOORCA_DATABASE_DSN` |
| `providers.openai.api_key` | `GOORCA_PROVIDERS_OPENAI_API_KEY` |
| `workflow.handoff_timeout` | `GOORCA_WORKFLOW_HANDOFF_TIMEOUT` |

---

## server

HTTP server settings.

| Key | Type | Default | Description |
|---|---|---|---|
| `server.host` | string | `"0.0.0.0"` | Interface to bind to |
| `server.port` | integer | `8080` | TCP port |
| `server.mode` | string | `"release"` | Gin mode: `debug` \| `release` \| `test` |
| `server.read_timeout` | duration | `"30s"` | Maximum time to read the request |
| `server.write_timeout` | duration | `"60s"` | Maximum time to write the response |
| `server.shutdown_timeout` | duration | `"15s"` | Grace period for in-flight requests on shutdown |
| `server.trusted_proxies` | []string | `[]` | CIDR ranges or IPs of trusted reverse proxies |

Duration values accept Go duration strings: `"30s"`, `"5m"`, `"1h"`.

---

## database

Storage backend settings.

| Key | Type | Default | Description |
|---|---|---|---|
| `database.driver` | string | `"sqlite"` | `sqlite` \| `postgres` |
| `database.dsn` | string | `"go-orca.db"` | SQLite file path or PostgreSQL connection string |
| `database.max_open_conns` | integer | `25` | Maximum open connections in the pool |
| `database.max_idle_conns` | integer | `5` | Maximum idle connections in the pool |
| `database.conn_max_lifetime` | duration | `"5m"` | Maximum lifetime of a pooled connection |
| `database.migrations_path` | string | `"internal/storage/migrations"` | Directory containing migration SQL files |
| `database.auto_migrate` | bool | `true` | Run pending migrations on startup |

### SQLite example
```yaml
database:
  driver: "sqlite"
  dsn: "go-orca.db"
```

### PostgreSQL example
```yaml
database:
  driver: "postgres"
  dsn: "postgres://go-orca:secret@localhost:5432/go-orca?sslmode=disable"
```

### Validation

`allow_team: true` requires `allow_org: true`. Setting `allow_team` without `allow_org` is a startup error.

---

## logging

| Key | Type | Default | Description |
|---|---|---|---|
| `logging.level` | string | `"info"` | `debug` \| `info` \| `warn` \| `error` |
| `logging.format` | string | `"json"` | `json` (structured) \| `console` (human-readable) |

---

## scoping

Controls the multi-tenancy and scope hierarchy.

| Key | Type | Default | Description |
|---|---|---|---|
| `scoping.mode` | string | `"global"` | `global` \| `org` \| `team` \| `hosted` |
| `scoping.allow_global` | bool | `true` | Allow global-kind scopes |
| `scoping.allow_org` | bool | `false` | Allow org-kind scopes |
| `scoping.allow_team` | bool | `false` | Allow team-kind scopes |
| `scoping.require_team_parent_org` | bool | `true` | Team scopes must have an org as parent |
| `scoping.default_tenant` | string | `"default"` | Slug of the auto-created default tenant |
| `scoping.default_scope` | string | `"global"` | Slug of the auto-created default scope |

### Scoping Modes

| Mode | Description |
|---|---|
| `global` | Single global scope; no org or team hierarchy |
| `org` | Org scopes under global; no teams |
| `team` | Full tree: global → org → team |
| `hosted` | Multi-tenant SaaS deployment |

---

## providers

All providers default to `enabled: false`. Enable at least one before submitting workflows.

### providers.openai

| Key | Type | Default | Description |
|---|---|---|---|
| `providers.openai.enabled` | bool | `false` | Enable this provider |
| `providers.openai.api_key` | string | `""` | OpenAI API key (or `GOORCA_PROVIDERS_OPENAI_API_KEY`) |
| `providers.openai.base_url` | string | `""` | Override base URL (empty = `api.openai.com`) |
| `providers.openai.default_model` | string | `"gpt-4o"` | Default model for this provider |
| `providers.openai.timeout` | duration | `"120s"` | Per-request timeout |

### providers.ollama

| Key | Type | Default | Description |
|---|---|---|---|
| `providers.ollama.enabled` | bool | `false` | Enable this provider |
| `providers.ollama.host` | string | `"http://localhost:11434"` | Ollama server URL |
| `providers.ollama.default_model` | string | `"llama3"` | Default model |
| `providers.ollama.timeout` | duration | `"120s"` | Per-request timeout |

### providers.copilot

| Key | Type | Default | Description |
|---|---|---|---|
| `providers.copilot.enabled` | bool | `false` | Enable this provider |
| `providers.copilot.github_token` | string | `""` | GitHub PAT (or `GOORCA_PROVIDERS_COPILOT_GITHUB_TOKEN`) |
| `providers.copilot.cli_path` | string | `""` | Path to the `gh` CLI binary; empty = use `$PATH` |
| `providers.copilot.default_model` | string | `"gpt-4o"` | Default model |

---

## customizations

Defines one or more sources from which skills, agent personas, and prompt overlays are loaded. Sources are scanned in precedence order.

```yaml
customizations:
  sources:
    - name: "local-skills"
      type: "filesystem"
      root: "./customizations"
      precedence: 10
      enabled_types:
        - "skill"
        - "prompt"
      scope_slug: "global"
      refresh_seconds: 60
```

### Per-source fields

| Key | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Display name for logging |
| `type` | string | Yes | `filesystem` \| `repo` \| `git-mirror` \| `builtin` |
| `root` | string | Yes (for filesystem/repo) | Absolute or relative path to scan |
| `precedence` | integer | No | Lower number = higher priority. Default `0` |
| `enabled_types` | []string | No | Which kinds to load: `skill`, `agent`, `prompt`. Empty = all |
| `scope_slug` | string | No | Restrict this source to a specific scope slug |
| `refresh_seconds` | integer | No | Rescan interval (informational; snapshot is taken at workflow start) |

See [Customization](customization.md) for a full description of how sources are resolved.

---

## workflow

Workflow engine and scheduler settings.

| Key | Type | Default | Description |
|---|---|---|---|
| `workflow.max_concurrent_workflows` | integer | `10` | Maps to `Scheduler.Concurrency` |
| `workflow.max_concurrent_tasks` | integer | `50` | Maximum parallel tasks within a single workflow run |
| `workflow.default_persona_timeout_ms` | integer | `120000` | Per-persona execution timeout in milliseconds |
| `workflow.event_retention_days` | integer | `90` | How long to keep journal events in the database |
| `workflow.artifact_storage_path` | string | `"./artifacts"` | On-disk path for artifact files |
| `workflow.handoff_timeout` | duration | `"5m"` | Per-persona handoff timeout (overrides `default_persona_timeout_ms` when set) |

---

## Validation Rules

The following rules are enforced at startup; violation is a fatal error:

1. `database.driver` must be `sqlite` or `postgres`
2. `scoping.allow_team: true` requires `scoping.allow_org: true`

---

## Full Example

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  mode: "release"
  read_timeout: "30s"
  write_timeout: "60s"
  shutdown_timeout: "15s"
  trusted_proxies: []

database:
  driver: "sqlite"
  dsn: "go-orca.db"
  max_open_conns: 25
  max_idle_conns: 5
  conn_max_lifetime: "5m"
  auto_migrate: true

logging:
  level: "info"
  format: "json"

scoping:
  mode: "global"
  allow_global: true
  allow_org: false
  allow_team: false
  require_team_parent_org: true
  default_tenant: "default"
  default_scope: "global"

providers:
  openai:
    enabled: true
    api_key: ""          # set GOORCA_PROVIDERS_OPENAI_API_KEY
    default_model: "gpt-4o"
    timeout: "120s"
  ollama:
    enabled: false
    host: "http://localhost:11434"
    default_model: "llama3"
    timeout: "120s"
  copilot:
    enabled: false
    github_token: ""
    default_model: "gpt-4o"

customizations:
  sources: []

workflow:
  max_concurrent_workflows: 10
  max_concurrent_tasks: 50
  default_persona_timeout_ms: 120000
  event_retention_days: 90
  artifact_storage_path: "./artifacts"
  handoff_timeout: "5m"
```
