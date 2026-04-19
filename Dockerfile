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

# ─── Runtime stage ────────────────────────────────────────────────────────────
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary
COPY --from=builder /go-orca-api ./go-orca-api

# Copy built-in skills and agent overlays
COPY skills/ ./skills/
COPY customization/ ./customization/

# Non-root user
RUN useradd -u 1001 -M -s /sbin/nologin orca
USER orca

EXPOSE 8080

ENTRYPOINT ["/app/go-orca-api"]
