# mcp-java-toolchain — first-party go-orca MCP server exposing governed Java
# capabilities (mvn install / test / package, gradle assemble / test / build).

FROM golang:1.26-bookworm AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /mcp-java-toolchain ./cmd/mcp-java-toolchain

FROM eclipse-temurin:21-jdk-jammy

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates wget maven gradle \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --shell /usr/sbin/nologin --uid 10001 mcp

WORKDIR /app
COPY --from=builder /mcp-java-toolchain /usr/local/bin/mcp-java-toolchain

ENV MCP_LISTEN=":3000" \
    MCP_WORKSPACE_ROOT="/var/lib/go-orca/workspaces" \
    GRADLE_USER_HOME=/home/mcp/.gradle \
    MAVEN_OPTS="-Dmaven.repo.local=/home/mcp/.m2/repository"
RUN mkdir -p "$MCP_WORKSPACE_ROOT" "$GRADLE_USER_HOME" /home/mcp/.m2/repository \
    && chown -R mcp:mcp "$MCP_WORKSPACE_ROOT" /home/mcp

USER mcp
EXPOSE 3000
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:3000/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/mcp-java-toolchain"]
