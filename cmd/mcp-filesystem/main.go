// Command mcp-filesystem is the first-party go-orca MCP server that exposes
// workspace-scoped filesystem primitives (read, write, list, stat, mkdir).
//
// Every operation is constrained to MCP_WORKSPACE_ROOT through
// policy.ResolveWorkspacePath; relative paths attempting to escape the root
// are rejected before any syscall runs.  Reads are size-capped to prevent a
// single large file from exhausting memory; writes are size-capped to
// prevent runaway content from filling the workspace volume.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/mcp/policy"
	"github.com/go-orca/go-orca/internal/mcp/server"
)

// Per-call size limits.  Configurable via env so operators can adjust in
// k8s without rebuilding the image.
var (
	defaultReadCap  int64 = 1 << 20 // 1 MiB
	defaultWriteCap int64 = 8 << 20 // 8 MiB
)

func main() {
	listen := flag.String("listen", envOr("MCP_LISTEN", ":3000"), "address to listen on")
	workspaceRoot := flag.String("workspace-root", envOr("MCP_WORKSPACE_ROOT", "/var/lib/go-orca/workspaces"), "absolute workspace root")
	flag.Parse()

	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("logger: %v", err)
	}
	defer logger.Sync() //nolint:errcheck

	readCap := envInt("MCP_FILESYSTEM_READ_CAP", defaultReadCap)
	writeCap := envInt("MCP_FILESYSTEM_WRITE_CAP", defaultWriteCap)

	srv := server.New(server.Options{
		Name: "mcp-filesystem", Version: "0.1.0", Listen: *listen, Logger: logger,
	})
	register(srv, *workspaceRoot, readCap, writeCap, logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("mcp-filesystem starting",
		zap.String("listen", *listen),
		zap.String("workspace_root", *workspaceRoot),
		zap.Int64("read_cap_bytes", readCap),
		zap.Int64("write_cap_bytes", writeCap),
	)
	if err := srv.ListenAndServe(ctx); err != nil && err != context.Canceled {
		logger.Fatal("server failed", zap.Error(err))
	}
}

// ─── Argument and result types ───────────────────────────────────────────────

type readArgs struct {
	WorkspacePath string `json:"workspace_path"`
	Path          string `json:"path"`
}

type readResult struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated,omitempty"`
}

type writeArgs struct {
	WorkspacePath string `json:"workspace_path"`
	Path          string `json:"path"`
	Content       string `json:"content"`
	CreateDirs    bool   `json:"create_dirs,omitempty"`
}

type writeResult struct {
	Path         string `json:"path"`
	BytesWritten int    `json:"bytes_written"`
}

type listArgs struct {
	WorkspacePath string `json:"workspace_path"`
	Path          string `json:"path"`
}

type listEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

type listResult struct {
	Path    string      `json:"path"`
	Entries []listEntry `json:"entries"`
}

type statArgs struct {
	WorkspacePath string `json:"workspace_path"`
	Path          string `json:"path"`
}

type statResult struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	IsDir  bool   `json:"is_dir"`
	Size   int64  `json:"size"`
	Mode   string `json:"mode"`
}

type mkdirArgs struct {
	WorkspacePath string `json:"workspace_path"`
	Path          string `json:"path"`
	Parents       bool   `json:"parents,omitempty"`
}

type mkdirResult struct {
	Path    string `json:"path"`
	Created bool   `json:"created"`
}

// register wires the five workspace tools onto srv.
func register(srv *server.Server, root string, readCap, writeCap int64, logger *zap.Logger) {
	mcp := srv.MCPServer()

	sdkmcp.AddTool(mcp, &sdkmcp.Tool{
		Name: "workspace_read", Description: "Read a file from the workspace; size-capped.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, a readArgs) (*sdkmcp.CallToolResult, any, error) {
		abs, err := resolveJoin(root, a.WorkspacePath, a.Path)
		if err != nil {
			return nil, nil, err
		}
		f, err := os.Open(abs)
		if err != nil {
			return nil, nil, err
		}
		defer f.Close()
		buf := make([]byte, readCap)
		n, err := io.ReadFull(f, buf)
		truncated := false
		if err == nil {
			// readCap bytes consumed, possibly more remaining
			if extra, _ := io.CopyN(io.Discard, f, 1); extra > 0 {
				truncated = true
			}
			buf = buf[:n]
		} else if err == io.EOF || err == io.ErrUnexpectedEOF {
			buf = buf[:n]
		} else {
			return nil, nil, err
		}
		info, _ := os.Stat(abs)
		var size int64
		if info != nil {
			size = info.Size()
		}
		res := readResult{Path: a.Path, Content: string(buf), Size: size, Truncated: truncated}
		return jsonContent(res)
	})

	sdkmcp.AddTool(mcp, &sdkmcp.Tool{
		Name: "workspace_write", Description: "Write content to a file in the workspace; size-capped.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, a writeArgs) (*sdkmcp.CallToolResult, any, error) {
		if int64(len(a.Content)) > writeCap {
			return nil, nil, fmt.Errorf("content exceeds write cap of %d bytes", writeCap)
		}
		abs, err := resolveJoin(root, a.WorkspacePath, a.Path)
		if err != nil {
			return nil, nil, err
		}
		if a.CreateDirs {
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return nil, nil, err
			}
		}
		if err := os.WriteFile(abs, []byte(a.Content), 0o644); err != nil {
			return nil, nil, err
		}
		logger.Info("filesystem write", zap.String("path", a.Path), zap.Int("bytes", len(a.Content)))
		return jsonContent(writeResult{Path: a.Path, BytesWritten: len(a.Content)})
	})

	sdkmcp.AddTool(mcp, &sdkmcp.Tool{
		Name: "workspace_list", Description: "List entries in a workspace directory.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, a listArgs) (*sdkmcp.CallToolResult, any, error) {
		abs, err := resolveJoin(root, a.WorkspacePath, a.Path)
		if err != nil {
			return nil, nil, err
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			return nil, nil, err
		}
		out := listResult{Path: a.Path, Entries: make([]listEntry, 0, len(entries))}
		for _, e := range entries {
			info, _ := e.Info()
			var size int64
			if info != nil {
				size = info.Size()
			}
			out.Entries = append(out.Entries, listEntry{Name: e.Name(), IsDir: e.IsDir(), Size: size})
		}
		return jsonContent(out)
	})

	sdkmcp.AddTool(mcp, &sdkmcp.Tool{
		Name: "workspace_stat", Description: "Stat a path in the workspace.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, a statArgs) (*sdkmcp.CallToolResult, any, error) {
		abs, err := resolveJoin(root, a.WorkspacePath, a.Path)
		if err != nil {
			return nil, nil, err
		}
		info, err := os.Stat(abs)
		if os.IsNotExist(err) {
			return jsonContent(statResult{Path: a.Path, Exists: false})
		}
		if err != nil {
			return nil, nil, err
		}
		return jsonContent(statResult{
			Path: a.Path, Exists: true, IsDir: info.IsDir(),
			Size: info.Size(), Mode: info.Mode().String(),
		})
	})

	sdkmcp.AddTool(mcp, &sdkmcp.Tool{
		Name: "workspace_mkdir", Description: "Create a directory inside the workspace.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, a mkdirArgs) (*sdkmcp.CallToolResult, any, error) {
		abs, err := resolveJoin(root, a.WorkspacePath, a.Path)
		if err != nil {
			return nil, nil, err
		}
		if a.Parents {
			err = os.MkdirAll(abs, 0o755)
		} else {
			err = os.Mkdir(abs, 0o755)
		}
		if err != nil && !os.IsExist(err) {
			return nil, nil, err
		}
		return jsonContent(mkdirResult{Path: a.Path, Created: err == nil})
	})
}

// resolveJoin resolves the workspace-relative path within the workspace root.
// Both `workspace_path` (the per-workflow subdir under the root) and the
// per-tool `path` are validated for escape.
func resolveJoin(root, workspacePath, rel string) (string, error) {
	wsAbs, err := policy.ResolveWorkspacePath(root, workspacePath)
	if err != nil {
		return "", err
	}
	abs, err := policy.ResolveWorkspacePath(wsAbs, rel)
	if err != nil {
		return "", err
	}
	return abs, nil
}

func jsonContent(v any) (*sdkmcp.CallToolResult, any, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(body)}},
	}, nil, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}
