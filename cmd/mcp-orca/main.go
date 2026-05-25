// Command mcp-orca is the go-orca MCP bridge for external agents (Cursor, VS Code, Claude).
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/mcp/orcabridge"
	"github.com/go-orca/go-orca/internal/mcp/server"
)

func main() {
	listen := flag.String("listen", envOr("MCP_LISTEN", ":3000"), "listen address")
	apiBase := flag.String("api-base", envOr("ORCA_API_BASE_URL", "http://127.0.0.1:8080"), "go-orca-api base URL")
	apiKey := flag.String("api-key", envOr("GOORCA_MCP_API_KEY", ""), "Bearer API key")
	tenantID := flag.String("tenant-id", envOr("GOORCA_TENANT_ID", ""), "default tenant")
	scopeID := flag.String("scope-id", envOr("GOORCA_SCOPE_ID", ""), "default scope")
	flag.Parse()

	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	defer logger.Sync() //nolint:errcheck

	client := orcabridge.NewClient(orcabridge.Config{
		BaseURL:         *apiBase,
		APIKey:          *apiKey,
		DefaultTenantID: *tenantID,
		DefaultScopeID:  *scopeID,
		HTTPTimeout:     10 * time.Minute,
	})

	srv := server.New(server.Options{
		Name:    "mcp-orca",
		Version: "0.1.0",
		Listen:  *listen,
		Logger:  logger,
	})
	orcabridge.RegisterTools(srv, client)
	orcabridge.RegisterGuidance(srv)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("mcp-orca listening", zap.String("addr", *listen), zap.String("api", *apiBase))
	if err := srv.ListenAndServe(ctx); err != nil {
		logger.Fatal("server error", zap.Error(err))
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
