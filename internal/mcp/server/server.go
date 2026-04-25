// Package server provides the shared HTTP/MCP server framework used by every
// first-party go-orca MCP server (mcp-go-toolchain, mcp-node-toolchain, …).
//
// It wraps the official Go MCP SDK's StreamableHTTPHandler with:
//   - a /healthz endpoint for Kubernetes liveness/readiness
//   - structured Zap logging for tool registration and dispatch
//   - a thin Capability registration helper that wires a typed handler
//     returning [capabilities.Result] (or [capabilities.CheckpointResult])
//     into an MCP tool that returns a single TextContent JSON payload
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/mcp/capabilities"
)

// Options configures a [Server].
type Options struct {
	// Name is the MCP server name reported during initialize.
	Name string
	// Version is the server version.  Defaults to "0.1.0" when empty.
	Version string
	// Listen is the address to bind, e.g. ":3000".  Defaults to ":3000".
	Listen string
	// Logger is the structured logger for tool dispatch.  Defaults to
	// zap.NewNop().
	Logger *zap.Logger
	// ReadHeaderTimeout for the HTTP server.  Defaults to 10s.
	ReadHeaderTimeout time.Duration
}

// Server is a single MCP server bound to one HTTP listener.
type Server struct {
	opts   Options
	mcp    *sdkmcp.Server
	logger *zap.Logger
}

// New constructs a Server.  Tools are added via [AddCapability] /
// [AddCheckpointCapability] before [Server.ListenAndServe] is called.
func New(opts Options) *Server {
	if opts.Version == "" {
		opts.Version = "0.1.0"
	}
	if opts.Listen == "" {
		opts.Listen = ":3000"
	}
	if opts.Logger == nil {
		opts.Logger = zap.NewNop()
	}
	if opts.ReadHeaderTimeout <= 0 {
		opts.ReadHeaderTimeout = 10 * time.Second
	}
	return &Server{
		opts:   opts,
		mcp:    sdkmcp.NewServer(&sdkmcp.Implementation{Name: opts.Name, Version: opts.Version}, nil),
		logger: opts.Logger,
	}
}

// MCPServer exposes the underlying SDK server so callers can register
// resources/prompts that go beyond the capability helpers.
func (s *Server) MCPServer() *sdkmcp.Server { return s.mcp }

// CapabilityHandler is a typed handler that produces a [capabilities.Result].
// Args is decoded from the MCP CallToolRequest into the standard
// [capabilities.Args] envelope.  Servers extract the workspace path / phase
// from Args and call into the policy package.
type CapabilityHandler func(ctx context.Context, args capabilities.Args) capabilities.Result

// CheckpointHandler is the equivalent for git-checkpoint capabilities.
type CheckpointHandler func(ctx context.Context, args capabilities.Args) (capabilities.CheckpointResult, error)

// AddCapability registers a single capability tool.
//
// toolName must be unique per server.  description is shown in tools/list.
// The input schema is auto-derived from [capabilities.Args] by the SDK.
func AddCapability(s *Server, toolName, description string, handler CapabilityHandler) {
	tool := &sdkmcp.Tool{Name: toolName, Description: description}
	sdkmcp.AddTool(s.mcp, tool, func(ctx context.Context, req *sdkmcp.CallToolRequest, args capabilities.Args) (*sdkmcp.CallToolResult, any, error) {
		s.logger.Info("mcp tool invoked",
			zap.String("tool", toolName),
			zap.String("workflow_id", args.WorkflowID),
			zap.String("capability", args.Capability),
			zap.String("phase", args.Phase),
		)
		res := handler(ctx, args)
		body, err := capabilities.MarshalResult(res)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal result: %w", err)
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(body)}},
			IsError: !res.Success,
		}, nil, nil
	})
}

// AddCheckpointCapability registers a checkpoint capability tool that returns
// a [capabilities.CheckpointResult].
func AddCheckpointCapability(s *Server, toolName, description string, handler CheckpointHandler) {
	tool := &sdkmcp.Tool{Name: toolName, Description: description}
	sdkmcp.AddTool(s.mcp, tool, func(ctx context.Context, req *sdkmcp.CallToolRequest, args capabilities.Args) (*sdkmcp.CallToolResult, any, error) {
		s.logger.Info("mcp checkpoint invoked",
			zap.String("tool", toolName),
			zap.String("workflow_id", args.WorkflowID),
			zap.String("phase", args.Phase),
		)
		res, err := handler(ctx, args)
		if err != nil {
			return nil, nil, err
		}
		body, err := capabilities.MarshalCheckpoint(res)
		if err != nil {
			return nil, nil, fmt.Errorf("marshal checkpoint: %w", err)
		}
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(body)}},
		}, nil, nil
	})
}

// ListenAndServe starts the HTTP server, blocking until ctx is cancelled or
// the listener errors.  /mcp serves the streamable MCP transport; /healthz
// returns 200 OK once the server is ready to accept requests.
func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.Handle("/mcp", sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return s.mcp
	}, nil))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	httpSrv := &http.Server{
		Addr:              s.opts.Listen,
		Handler:           mux,
		ReadHeaderTimeout: s.opts.ReadHeaderTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("mcp server listening",
			zap.String("name", s.opts.Name),
			zap.String("addr", s.opts.Listen),
		)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

