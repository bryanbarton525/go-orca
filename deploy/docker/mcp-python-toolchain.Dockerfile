# mcp-python-toolchain — first-party go-orca MCP server exposing governed
# Python capabilities (pip install, ruff, pytest, mypy, uv).

FROM golang:1.26-bookworm AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /mcp-python-toolchain ./cmd/mcp-python-toolchain

FROM python:3.12-slim-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates wget \
    && rm -rf /var/lib/apt/lists/* \
    && pip install --no-cache-dir --upgrade pip \
    && pip install --no-cache-dir ruff pytest mypy uv \
    && useradd --create-home --shell /usr/sbin/nologin --uid 10001 mcp

WORKDIR /app
COPY --from=builder /mcp-python-toolchain /usr/local/bin/mcp-python-toolchain

ENV MCP_LISTEN=":3000" \
    MCP_WORKSPACE_ROOT="/var/lib/go-orca/workspaces" \
    PIP_CACHE_DIR=/home/mcp/.cache/pip \
    UV_CACHE_DIR=/home/mcp/.cache/uv \
    PYTHONUNBUFFERED=1
RUN mkdir -p "$MCP_WORKSPACE_ROOT" "$PIP_CACHE_DIR" "$UV_CACHE_DIR" \
    && chown -R mcp:mcp "$MCP_WORKSPACE_ROOT" /home/mcp

USER mcp
EXPOSE 3000
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:3000/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/mcp-python-toolchain"]
