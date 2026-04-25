# mcp-container-build — optional first-party go-orca MCP server exposing
# container-image capabilities (dockerfile_lint via hadolint, container_build
# and container_push via buildah).  Buildah is daemonless and rootless-
# friendly so this server can run as a non-privileged pod.

FROM golang:1.26-bookworm AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /mcp-container-build ./cmd/mcp-container-build

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates wget buildah \
    && rm -rf /var/lib/apt/lists/* \
    && wget -qO /usr/local/bin/hadolint \
       https://github.com/hadolint/hadolint/releases/download/v2.12.0/hadolint-Linux-x86_64 \
    && chmod +x /usr/local/bin/hadolint \
    && useradd --create-home --shell /usr/sbin/nologin --uid 10001 mcp

WORKDIR /app
COPY --from=builder /mcp-container-build /usr/local/bin/mcp-container-build

ENV MCP_LISTEN=":3000" \
    MCP_WORKSPACE_ROOT="/var/lib/go-orca/workspaces" \
    BUILDAH_ISOLATION=chroot
RUN mkdir -p "$MCP_WORKSPACE_ROOT" && chown -R mcp:mcp "$MCP_WORKSPACE_ROOT" /home/mcp

USER mcp
EXPOSE 3000
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:3000/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/mcp-container-build"]
