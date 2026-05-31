FROM golang:1.26-bookworm AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /mcp-orca ./cmd/mcp-orca

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates wget \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --shell /usr/sbin/nologin --uid 10001 mcp
COPY --from=builder /mcp-orca /usr/local/bin/mcp-orca
ENV MCP_LISTEN=":3000" ORCA_API_BASE_URL="http://go-orca-api:8080"
USER mcp
EXPOSE 3000
HEALTHCHECK CMD wget -q --spider http://127.0.0.1:3000/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/mcp-orca"]
