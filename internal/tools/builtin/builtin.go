// Package builtin provides a set of lightweight built-in tools that are
// compiled into the gorca binary and registered at startup.
//
// Available tools:
//   - http_get   — performs an HTTP GET and returns the response body as a string
//   - read_file  — reads a file from the local filesystem
//   - write_file — writes content to a file on the local filesystem
//
// All three implement the tools.Tool interface and are safe to register into
// tools.Global via RegisterAll.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-orca/go-orca/internal/tools"
)

// RegisterAll registers all built-in tools into the given registry.
func RegisterAll(reg *tools.Registry) {
	reg.Register(&HTTPGetTool{client: &http.Client{Timeout: 30 * time.Second}})
	reg.Register(&ReadFileTool{})
	reg.Register(&WriteFileTool{})
}

// ─── http_get ─────────────────────────────────────────────────────────────────

// HTTPGetTool performs an HTTP GET request and returns the response body.
type HTTPGetTool struct {
	client *http.Client
}

var _ tools.Tool = (*HTTPGetTool)(nil)

func (t *HTTPGetTool) Name() string { return "http_get" }

func (t *HTTPGetTool) Description() string {
	return "Performs an HTTP GET request to the given URL and returns the response body as a UTF-8 string."
}

func (t *HTTPGetTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "url": {
      "type": "string",
      "description": "The URL to fetch (must be http or https)."
    },
    "headers": {
      "type": "object",
      "description": "Optional map of request headers to include.",
      "additionalProperties": { "type": "string" }
    }
  },
  "required": ["url"]
}`)
}

type httpGetArgs struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

type httpGetResult struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}

func (t *HTTPGetTool) Call(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a httpGetArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("http_get: invalid args: %w", err)
	}
	if a.URL == "" {
		return nil, fmt.Errorf("http_get: url is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("http_get: build request: %w", err)
	}
	for k, v := range a.Headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http_get: request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("http_get: read body: %w", err)
	}

	return json.Marshal(httpGetResult{
		StatusCode: resp.StatusCode,
		Body:       string(body),
	})
}

// ─── read_file ────────────────────────────────────────────────────────────────

// ReadFileTool reads a file from the local filesystem.
type ReadFileTool struct{}

var _ tools.Tool = (*ReadFileTool)(nil)

func (t *ReadFileTool) Name() string { return "read_file" }

func (t *ReadFileTool) Description() string {
	return "Reads a file from the local filesystem and returns its contents as a UTF-8 string."
}

func (t *ReadFileTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Absolute or relative path to the file to read."
    }
  },
  "required": ["path"]
}`)
}

type readFileArgs struct {
	Path string `json:"path"`
}

type readFileResult struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Size    int64  `json:"size_bytes"`
}

func (t *ReadFileTool) Call(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a readFileArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("read_file: invalid args: %w", err)
	}
	if a.Path == "" {
		return nil, fmt.Errorf("read_file: path is required")
	}

	clean := filepath.Clean(a.Path)
	data, err := os.ReadFile(clean)
	if err != nil {
		return nil, fmt.Errorf("read_file: %w", err)
	}

	return json.Marshal(readFileResult{
		Path:    clean,
		Content: string(data),
		Size:    int64(len(data)),
	})
}

// ─── write_file ───────────────────────────────────────────────────────────────

// WriteFileTool writes content to a file on the local filesystem.
type WriteFileTool struct{}

var _ tools.Tool = (*WriteFileTool)(nil)

func (t *WriteFileTool) Name() string { return "write_file" }

func (t *WriteFileTool) Description() string {
	return "Writes a UTF-8 string to a file on the local filesystem. Creates parent directories as needed."
}

func (t *WriteFileTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Absolute or relative path to the file to write."
    },
    "content": {
      "type": "string",
      "description": "UTF-8 content to write to the file."
    },
    "append": {
      "type": "boolean",
      "description": "If true, append to the file instead of overwriting. Defaults to false.",
      "default": false
    }
  },
  "required": ["path", "content"]
}`)
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Append  bool   `json:"append"`
}

type writeFileResult struct {
	Path         string `json:"path"`
	BytesWritten int    `json:"bytes_written"`
}

func (t *WriteFileTool) Call(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a writeFileArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("write_file: invalid args: %w", err)
	}
	if a.Path == "" {
		return nil, fmt.Errorf("write_file: path is required")
	}

	clean := filepath.Clean(a.Path)

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
		return nil, fmt.Errorf("write_file: mkdir: %w", err)
	}

	flag := os.O_CREATE | os.O_WRONLY
	if a.Append {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}

	f, err := os.OpenFile(clean, flag, 0o644)
	if err != nil {
		return nil, fmt.Errorf("write_file: open: %w", err)
	}
	defer f.Close() //nolint:errcheck

	n, err := f.WriteString(a.Content)
	if err != nil {
		return nil, fmt.Errorf("write_file: write: %w", err)
	}

	return json.Marshal(writeFileResult{
		Path:         clean,
		BytesWritten: n,
	})
}
