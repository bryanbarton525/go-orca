# mcp-go-toolchain — first-party go-orca MCP server exposing governed Go
# toolchain capabilities (init_project, tidy_dependencies, format_code,
# run_tests, run_build, run_lint).  Workspace-confined and policy-gated.

# ─── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.26-bookworm AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /mcp-go-toolchain ./cmd/mcp-go-toolchain

# ─── Runtime stage ────────────────────────────────────────────────────────────
# Runtime carries the same Go toolchain version the server was built against,
# because the MCP capabilities (go test, go build, gofmt, go vet) are run
# against the *workspace's* Go module — the binary itself is a thin shim.
FROM golang:1.26-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --shell /usr/sbin/nologin --uid 10001 mcp

WORKDIR /app
COPY --from=builder /mcp-go-toolchain /usr/local/bin/mcp-go-toolchain

# Workspace volume mount (must match go-orca-api's workflow.workspace_root).
ENV MCP_LISTEN=":3000" \
    MCP_WORKSPACE_ROOT="/var/lib/go-orca/workspaces" \
    GOCACHE=/home/mcp/.cache/go-build \
    GOMODCACHE=/home/mcp/go/pkg/mod
RUN mkdir -p "$MCP_WORKSPACE_ROOT" "$GOCACHE" "$GOMODCACHE" \
    && chown -R mcp:mcp "$MCP_WORKSPACE_ROOT" /home/mcp

USER mcp

EXPOSE 3000

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:3000/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/mcp-go-toolchain"]
