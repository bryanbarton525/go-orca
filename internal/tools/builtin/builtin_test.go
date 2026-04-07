package builtin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-orca/go-orca/internal/tools"
	"github.com/go-orca/go-orca/internal/tools/builtin"
)

// helper: marshal args to json.RawMessage
func args(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// helper: unmarshal a raw result into a map
func resultMap(t *testing.T, raw json.RawMessage) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return m
}

// ─── RegisterAll ──────────────────────────────────────────────────────────────

func TestRegisterAll(t *testing.T) {
	reg := tools.NewRegistry()
	builtin.RegisterAll(reg)

	for _, name := range []string{"http_get", "read_file", "write_file"} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("RegisterAll: tool %q not found in registry", name)
		}
	}
}

// ─── http_get ─────────────────────────────────────────────────────────────────

func TestHTTPGetTool_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	reg := tools.NewRegistry()
	builtin.RegisterAll(reg)

	tool, ok := reg.Get("http_get")
	if !ok {
		t.Fatal("http_get not registered")
	}

	raw, err := tool.Call(context.Background(), args(map[string]string{"url": srv.URL}))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	m := resultMap(t, raw)
	if m["status_code"] != float64(200) {
		t.Errorf("status_code: got %v, want 200", m["status_code"])
	}
	if m["body"] != "hello world" {
		t.Errorf("body: got %q, want %q", m["body"], "hello world")
	}
}

func TestHTTPGetTool_WithHeaders(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	reg := tools.NewRegistry()
	builtin.RegisterAll(reg)
	tool, _ := reg.Get("http_get")

	_, err := tool.Call(context.Background(), args(map[string]interface{}{
		"url":     srv.URL,
		"headers": map[string]string{"X-Custom": "test-value"},
	}))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotHeader != "test-value" {
		t.Errorf("X-Custom header: got %q, want %q", gotHeader, "test-value")
	}
}

func TestHTTPGetTool_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer srv.Close()

	reg := tools.NewRegistry()
	builtin.RegisterAll(reg)
	tool, _ := reg.Get("http_get")

	raw, err := tool.Call(context.Background(), args(map[string]string{"url": srv.URL}))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	m := resultMap(t, raw)
	if m["status_code"] != float64(404) {
		t.Errorf("status_code: got %v, want 404", m["status_code"])
	}
}

func TestHTTPGetTool_MissingURL(t *testing.T) {
	reg := tools.NewRegistry()
	builtin.RegisterAll(reg)
	tool, _ := reg.Get("http_get")

	_, err := tool.Call(context.Background(), args(map[string]string{}))
	if err == nil {
		t.Error("expected error for missing url, got nil")
	}
}

func TestHTTPGetTool_InvalidArgs(t *testing.T) {
	reg := tools.NewRegistry()
	builtin.RegisterAll(reg)
	tool, _ := reg.Get("http_get")

	_, err := tool.Call(context.Background(), json.RawMessage(`not-json`))
	if err == nil {
		t.Error("expected error for invalid JSON args, got nil")
	}
}

// ─── read_file ────────────────────────────────────────────────────────────────

func TestReadFileTool_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("file content"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := tools.NewRegistry()
	builtin.RegisterAll(reg)
	tool, _ := reg.Get("read_file")

	raw, err := tool.Call(context.Background(), args(map[string]string{"path": path}))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	m := resultMap(t, raw)
	if m["content"] != "file content" {
		t.Errorf("content: got %q, want %q", m["content"], "file content")
	}
	if m["size_bytes"] != float64(len("file content")) {
		t.Errorf("size_bytes: got %v, want %d", m["size_bytes"], len("file content"))
	}
}

func TestReadFileTool_NotFound(t *testing.T) {
	reg := tools.NewRegistry()
	builtin.RegisterAll(reg)
	tool, _ := reg.Get("read_file")

	_, err := tool.Call(context.Background(), args(map[string]string{"path": "/nonexistent/path/file.txt"}))
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestReadFileTool_MissingPath(t *testing.T) {
	reg := tools.NewRegistry()
	builtin.RegisterAll(reg)
	tool, _ := reg.Get("read_file")

	_, err := tool.Call(context.Background(), args(map[string]string{}))
	if err == nil {
		t.Error("expected error for missing path, got nil")
	}
}

// ─── write_file ───────────────────────────────────────────────────────────────

func TestWriteFileTool_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	reg := tools.NewRegistry()
	builtin.RegisterAll(reg)
	tool, _ := reg.Get("write_file")

	raw, err := tool.Call(context.Background(), args(map[string]interface{}{
		"path":    path,
		"content": "written content",
	}))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	m := resultMap(t, raw)
	if m["bytes_written"] != float64(len("written content")) {
		t.Errorf("bytes_written: got %v, want %d", m["bytes_written"], len("written content"))
	}

	data, _ := os.ReadFile(path)
	if string(data) != "written content" {
		t.Errorf("file content: got %q, want %q", string(data), "written content")
	}
}

func TestWriteFileTool_Append(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "append.txt")

	reg := tools.NewRegistry()
	builtin.RegisterAll(reg)
	tool, _ := reg.Get("write_file")

	// First write
	_, err := tool.Call(context.Background(), args(map[string]interface{}{
		"path":    path,
		"content": "line1\n",
	}))
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Append
	_, err = tool.Call(context.Background(), args(map[string]interface{}{
		"path":    path,
		"content": "line2\n",
		"append":  true,
	}))
	if err != nil {
		t.Fatalf("append: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "line1\nline2\n" {
		t.Errorf("appended content: got %q", string(data))
	}
}

func TestWriteFileTool_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "file.txt")

	reg := tools.NewRegistry()
	builtin.RegisterAll(reg)
	tool, _ := reg.Get("write_file")

	_, err := tool.Call(context.Background(), args(map[string]interface{}{
		"path":    path,
		"content": "nested",
	}))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "nested" {
		t.Errorf("content: got %q", string(data))
	}
}

func TestWriteFileTool_MissingPath(t *testing.T) {
	reg := tools.NewRegistry()
	builtin.RegisterAll(reg)
	tool, _ := reg.Get("write_file")

	_, err := tool.Call(context.Background(), args(map[string]interface{}{
		"content": "hello",
	}))
	if err == nil {
		t.Error("expected error for missing path, got nil")
	}
}

// ─── Tool interface metadata ──────────────────────────────────────────────────

func TestToolMetadata(t *testing.T) {
	reg := tools.NewRegistry()
	builtin.RegisterAll(reg)

	for _, name := range []string{"http_get", "read_file", "write_file"} {
		tool, ok := reg.Get(name)
		if !ok {
			t.Errorf("tool %q not found", name)
			continue
		}
		if tool.Name() != name {
			t.Errorf("%s: Name() = %q", name, tool.Name())
		}
		if tool.Description() == "" {
			t.Errorf("%s: Description() is empty", name)
		}
		params := tool.Parameters()
		if len(params) == 0 {
			t.Errorf("%s: Parameters() is empty", name)
		}
		var schema map[string]interface{}
		if err := json.Unmarshal(params, &schema); err != nil {
			t.Errorf("%s: Parameters() is not valid JSON: %v", name, err)
		}
	}
}
