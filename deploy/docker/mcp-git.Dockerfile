# mcp-git — first-party go-orca MCP server exposing governed git capabilities
# (git_status, git_checkpoint, git_push_checkpoint).  Workspace-confined and
# allowlisted to a fixed set of git subcommands.

# ─── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.26-bookworm AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /mcp-git ./cmd/mcp-git

# ─── Runtime stage ────────────────────────────────────────────────────────────
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates git openssh-client wget \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --shell /usr/sbin/nologin --uid 10001 mcp

WORKDIR /app
COPY --from=builder /mcp-git /usr/local/bin/mcp-git

ENV MCP_LISTEN=":3000" \
    MCP_WORKSPACE_ROOT="/var/lib/go-orca/workspaces"
RUN mkdir -p "$MCP_WORKSPACE_ROOT" && chown -R mcp:mcp "$MCP_WORKSPACE_ROOT" /home/mcp

USER mcp
EXPOSE 3000
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:3000/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/mcp-git"]
