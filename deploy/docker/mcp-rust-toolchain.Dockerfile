# mcp-rust-toolchain — first-party go-orca MCP server exposing governed Rust
# capabilities (cargo update / fmt / test / build / clippy / check).

FROM golang:1.26-bookworm AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /mcp-rust-toolchain ./cmd/mcp-rust-toolchain

FROM rust:1-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates wget \
    && rm -rf /var/lib/apt/lists/* \
    && rustup component add rustfmt clippy \
    && useradd --create-home --shell /usr/sbin/nologin --uid 10001 mcp

WORKDIR /app
COPY --from=builder /mcp-rust-toolchain /usr/local/bin/mcp-rust-toolchain

ENV MCP_LISTEN=":3000" \
    MCP_WORKSPACE_ROOT="/var/lib/go-orca/workspaces" \
    CARGO_HOME=/home/mcp/.cargo \
    RUSTUP_HOME=/usr/local/rustup
RUN mkdir -p "$MCP_WORKSPACE_ROOT" "$CARGO_HOME" \
    && chown -R mcp:mcp "$MCP_WORKSPACE_ROOT" /home/mcp

USER mcp
EXPOSE 3000
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:3000/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/mcp-rust-toolchain"]
