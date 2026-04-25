# mcp-nextjs-toolchain — first-party go-orca MCP server exposing governed
# Next.js capabilities (pnpm install, next build, next lint, tsc, prettier,
# test).  Workspace-confined and policy-gated.

# ─── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.26-bookworm AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /mcp-nextjs-toolchain ./cmd/mcp-nextjs-toolchain

# ─── Runtime stage ────────────────────────────────────────────────────────────
# node:22-bookworm-slim ships node + npm; pnpm is installed globally.
# NEXT_TELEMETRY_DISABLED suppresses telemetry calls during build/lint.
FROM node:22-bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates wget \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g pnpm@10 --silent \
    && useradd --create-home --shell /usr/sbin/nologin --uid 10001 mcp

WORKDIR /app
COPY --from=builder /mcp-nextjs-toolchain /usr/local/bin/mcp-nextjs-toolchain

ENV MCP_LISTEN=":3000" \
    MCP_WORKSPACE_ROOT="/var/lib/go-orca/workspaces" \
    NPM_CONFIG_CACHE=/home/mcp/.npm \
    PNPM_HOME=/home/mcp/.local/share/pnpm \
    NEXT_TELEMETRY_DISABLED=1

RUN mkdir -p "$MCP_WORKSPACE_ROOT" "$NPM_CONFIG_CACHE" "$PNPM_HOME" \
    && chown -R mcp:mcp "$MCP_WORKSPACE_ROOT" /home/mcp

USER mcp
EXPOSE 3000
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:3000/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/mcp-nextjs-toolchain"]
