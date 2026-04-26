# ─── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.26-bookworm AS builder

WORKDIR /build

# Download deps first for layer caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build with CGO enabled (required for go-sqlite3)
COPY . .
RUN CGO_ENABLED=1 GOOS=linux \
    go build -ldflags="-s -w" -o /go-orca-api ./cmd/go-orca-api

# ─── Copilot CLI stage ────────────────────────────────────────────────────────
FROM debian:bookworm-slim AS copilot-cli

ARG COPILOT_CLI_VERSION=1.0.32
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates wget \
    && rm -rf /var/lib/apt/lists/* \
    && wget -qO /tmp/copilot.tar.gz \
       "https://github.com/github/copilot-cli/releases/download/v${COPILOT_CLI_VERSION}/copilot-linux-x64.tar.gz" \
    && tar -xzf /tmp/copilot.tar.gz -C /tmp \
    && chmod +x /tmp/copilot \
    && rm /tmp/copilot.tar.gz

# ─── Runtime stage ────────────────────────────────────────────────────────────
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    wget \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary
COPY --from=builder /go-orca-api ./go-orca-api

# Copy copilot CLI binary
COPY --from=copilot-cli /tmp/copilot /usr/local/bin/copilot

# Copy built-in skills, agent overlays, persona prompts, and DB migrations
COPY skills/ ./skills/
COPY customization/ ./customization/
COPY prompts/ ./prompts/
COPY internal/storage/migrations/ ./internal/storage/migrations/

# Non-root user.  uid/gid 10001 is shared with every first-party MCP server in
# deploy/docker/mcp-*.Dockerfile so files written by the API into the shared
# workspace PVC are readable/writable by every container that mounts it.
# Without this alignment, gofmt and git checkpoint operations from the MCP
# containers fail with "permission denied" on temp files and .git creation.
RUN useradd -u 10001 -m -s /sbin/nologin orca
USER orca

EXPOSE 8080

ENTRYPOINT ["/app/go-orca-api"]
