# Deployment

This document covers building, running, and operating go-orca in homelab and production environments.

## Prerequisites

- Go 1.21 or later (module path requires Go 1.21+; `go.mod` declares `go 1.26.1`)
- A running LLM provider (OpenAI API key, local Ollama, or GitHub Copilot token)
- SQLite (default, no setup needed) or a PostgreSQL 14+ instance

---

## Build

```bash
git clone https://github.com/go-orca/go-orca.git
cd go-orca
go build -o go-orca-api ./cmd/go-orca-api
```

### Cross-compile

```bash
GOOS=linux GOARCH=amd64 go build -o go-orca-api-linux-amd64 ./cmd/go-orca-api
```

---

## Configuration

Copy the example config and edit it:

```bash
cp go-orca.yaml /etc/go-orca/go-orca.yaml
```

Minimum viable config (SQLite + OpenAI):

```yaml
providers:
  openai:
    enabled: true
    api_key: "sk-..."
```

All other values use defaults. See [Configuration](configuration.md) for the full reference.

### Config File Search Paths

If `-config` is not passed, the server looks for `go-orca.yaml` in order:

1. `./go-orca.yaml` (current working directory)
2. `/etc/go-orca/go-orca.yaml`
3. `$HOME/.go-orca/go-orca.yaml`

---

## Running the Binary

```bash
# With default config search
./go-orca-api

# With explicit config path
./go-orca-api -config /etc/go-orca/go-orca.yaml
```

The server logs startup progress in JSON format and begins accepting requests on `http://0.0.0.0:8080`.

### Environment Variable Overrides

All config keys can be set via `GOORCA_*` environment variables:

```bash
export GOORCA_SERVER_PORT=9090
export GOORCA_DATABASE_DSN="postgres://..."
export GOORCA_PROVIDERS_OPENAI_API_KEY="sk-..."
./go-orca-api
```

---

## Running as a systemd Service

Create `/etc/systemd/system/go-orca.service`:

```ini
[Unit]
Description=go-orca AI workflow orchestration server
After=network.target

[Service]
Type=simple
User=go-orca
Group=go-orca
WorkingDirectory=/opt/go-orca
ExecStart=/opt/go-orca/go-orca-api -config /etc/go-orca/go-orca.yaml
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal
Environment=GOORCA_PROVIDERS_OPENAI_API_KEY=sk-...

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now go-orca
sudo journalctl -u go-orca -f
```

---

## Docker

### Dockerfile (minimal)

```dockerfile
FROM golang:1.21-alpine AS build
WORKDIR /src
COPY . .
RUN go build -o /go-orca-api ./cmd/go-orca-api

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /go-orca-api .
COPY go-orca.yaml .
EXPOSE 8080
ENTRYPOINT ["/app/go-orca-api", "-config", "/app/go-orca.yaml"]
```

### Docker Compose (with PostgreSQL)

```yaml
version: "3.9"
services:
  db:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: go-orca
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: go-orca
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U go-orca"]
      interval: 5s
      timeout: 3s
      retries: 5

  go-orca:
    build: .
    ports:
      - "8080:8080"
    environment:
      GOORCA_DATABASE_DRIVER: postgres
      GOORCA_DATABASE_DSN: postgres://go-orca:secret@db:5432/go-orca?sslmode=disable
      GOORCA_PROVIDERS_OPENAI_ENABLED: "true"
      GOORCA_PROVIDERS_OPENAI_API_KEY: "${OPENAI_API_KEY}"
    depends_on:
      db:
        condition: service_healthy

volumes:
  pgdata:
```

```bash
export OPENAI_API_KEY=sk-...
docker compose up -d
```

---

## Reverse Proxy (nginx)

```nginx
server {
    listen 443 ssl;
    server_name orca.example.com;

    ssl_certificate     /etc/ssl/certs/orca.crt;
    ssl_certificate_key /etc/ssl/private/orca.key;

    location / {
        proxy_pass         http://127.0.0.1:8080;
        proxy_http_version 1.1;

        # Required for SSE streaming
        proxy_buffering    off;
        proxy_cache        off;
        proxy_read_timeout 360s;

        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Set `server.trusted_proxies` in your config to the nginx server's IP so go-orca trusts the `X-Forwarded-For` header:

```yaml
server:
  trusted_proxies:
    - "127.0.0.1"
```

**SSE note:** `proxy_buffering off` is required for `GET /workflows/:id/stream` to deliver events in real time.

---

## Graceful Shutdown

go-orca handles `SIGINT` and `SIGTERM`. On signal receipt:

1. `http.Server.Shutdown(ctx)` — stops accepting new connections, drains in-flight HTTP requests
2. `Scheduler.Shutdown(ctx)` — cancels the worker context, then waits (up to `shutdown_timeout`) for worker goroutines to exit

**Important:** `Scheduler.Shutdown` cancels the context immediately. In-flight workflow runs receive a context-cancellation signal and stop at the next LLM call boundary. Depending on timing, a workflow may be left in `running` or `failed` state rather than completing cleanly. Workflows in `failed` state can be resumed via `POST /workflows/:id/resume`.

Increase `shutdown_timeout` to give in-flight LLM calls time to complete before the process exits:

```yaml
server:
  shutdown_timeout: "2m"
```

---

## Health and Readiness Probes

| Endpoint | Purpose | Kubernetes probe type |
|---|---|---|
| `GET /healthz` | Liveness — returns 200 immediately | `livenessProbe` |
| `GET /readyz` | Readiness — returns 200 only if DB is reachable | `readinessProbe` |

Kubernetes example:

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /readyz
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 15
```

---

## Artifact Storage

Artifacts with on-disk content are stored at the path configured by `workflow.artifact_storage_path` (default: `./artifacts`). Ensure this directory:

- Exists and is writable by the process user
- Is on a persistent volume if running in Docker or Kubernetes

```yaml
workflow:
  artifact_storage_path: "/data/go-orca/artifacts"
```

---

## Production Checklist

- [ ] Set `server.mode: "release"` (default)
- [ ] Set `logging.format: "json"` and pipe logs to a log aggregator
- [ ] Use PostgreSQL (`database.driver: "postgres"`)
- [ ] Set `database.auto_migrate: true` on first deploy; consider disabling afterwards
- [ ] Set API keys via environment variables, not YAML files committed to version control
- [ ] Use `server.trusted_proxies` if behind a reverse proxy
- [ ] Set `server.shutdown_timeout` to at least the maximum expected workflow duration
- [ ] Mount `workflow.artifact_storage_path` on persistent storage
- [ ] Configure liveness and readiness probes
- [ ] Set `workflow.event_retention_days` to control database growth
