# Tools

go-orca personas can call tools during their execution. Tools are registered in a global `tools.Registry` and invoked by name with JSON arguments. go-orca ships three built-in tools and supports loading additional tools from any MCP-compatible server.

## Tool Interface

All tools implement `tools.Tool`:

```go
type Tool interface {
    Name()        string
    Description() string
    Parameters()  json.RawMessage   // JSON Schema for arguments
    Call(ctx context.Context, args json.RawMessage) (json.RawMessage, error)
}
```

Tools are registered at startup:

```go
toolReg := tools.NewRegistry()
builtin.RegisterAll(toolReg)
// optional: mcp.Load(toolReg, "http://mcp-server/manifest", mcp.LoaderOptions{})
```

---

## Built-in Tools

Package: `internal/tools/builtin`

### http_get

Performs an HTTP GET request and returns the response body.

**Parameters:**

```json
{
  "type": "object",
  "properties": {
    "url": {
      "type": "string",
      "description": "The URL to fetch (must be http or https)."
    },
    "headers": {
      "type": "object",
      "description": "Optional map of request headers.",
      "additionalProperties": { "type": "string" }
    }
  },
  "required": ["url"]
}
```

**Returns:**

```json
{
  "status_code": 200,
  "body": "<response body as UTF-8 string>"
}
```

**Timeout:** 30 seconds (fixed).

**Example call:**
```json
{ "url": "https://api.github.com/repos/golang/go", "headers": { "Accept": "application/json" } }
```

---

### read_file

Reads a file from the local filesystem.

**Parameters:**

```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Absolute or relative path to the file to read."
    }
  },
  "required": ["path"]
}
```

**Returns:**

```json
{
  "path": "/cleaned/path",
  "content": "<file contents as UTF-8>",
  "size_bytes": 1234
}
```

The path is cleaned with `filepath.Clean` before reading. Returns an error if the file does not exist or cannot be read.

---

### write_file

Writes a UTF-8 string to a file, creating parent directories as needed.

**Parameters:**

```json
{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Absolute or relative path to write."
    },
    "content": {
      "type": "string",
      "description": "UTF-8 content to write."
    },
    "append": {
      "type": "boolean",
      "description": "If true, append instead of overwriting. Default: false.",
      "default": false
    }
  },
  "required": ["path", "content"]
}
```

**Returns:**

```json
{
  "path": "/cleaned/path",
  "bytes_written": 512
}
```

File permissions: `0644`. Parent directory permissions: `0755`.

---

## MCP (Model Context Protocol) Tools

Package: `internal/tools/mcp`

go-orca can load additional tools from any HTTP server that exposes an MCP manifest. This lets you extend the tool set without modifying the binary.

### Manifest Format

Your MCP server must expose a JSON manifest at a URL (e.g. `http://localhost:8181/manifest`):

```json
{
  "name": "my-mcp-server",
  "version": "1.0.0",
  "tools": [
    {
      "name": "search_web",
      "description": "Searches the web and returns top results.",
      "parameters": {
        "type": "object",
        "properties": {
          "query": { "type": "string" }
        },
        "required": ["query"]
      },
      "endpoint": "/tools/search_web"
    }
  ]
}
```

| Field | Description |
|---|---|
| `name` | Server display name |
| `version` | Server version string |
| `tools[].name` | Tool name (must be unique across all registered tools) |
| `tools[].description` | Description exposed to personas |
| `tools[].parameters` | JSON Schema for arguments |
| `tools[].endpoint` | Path relative to manifest base URL to POST JSON-RPC calls to |

### JSON-RPC Call Protocol

go-orca calls each MCP tool via HTTP POST to its endpoint using JSON-RPC 2.0:

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "method": "search_web",
  "params": { "query": "Go generics tutorial" }
}
```

**Success response:**
```json
{
  "jsonrpc": "2.0",
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "result": { "results": [...] }
}
```

**Error response:**
```json
{
  "jsonrpc": "2.0",
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "error": { "code": -1, "message": "search failed: rate limited" }
}
```

### Loading MCP Tools

MCP loading is currently wired manually in `main.go` (not yet exposed via config). To add an MCP server:

```go
err := mcp.Load(toolReg, "http://localhost:8181/manifest", mcp.LoaderOptions{
    HTTPTimeout: 30 * time.Second,
})
```

- Endpoint paths are resolved relative to the manifest base URL (scheme + host).
- Absolute endpoint URLs in the manifest are used as-is.
- All tools from the manifest are registered under their `name` field.
- Duplicate names cause a panic at registration time.

### Loader Options

| Option | Default | Description |
|---|---|---|
| `HTTPTimeout` | 30s | Timeout for both the manifest fetch and individual tool calls |

---

## Registry

The global tool registry at startup:

```go
tools.Global
```

Use `toolReg.All()` to list registered tools, or `toolReg.Get("name")` to look up a specific tool.
