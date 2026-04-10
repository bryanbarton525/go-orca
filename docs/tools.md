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

// HTTP MCP server (Streamable transport, 2025-03-26 spec):
session, err := mcp.Load(ctx, toolReg, "http://localhost:3000/mcp", mcp.LoaderOptions{})
defer session.Close()

// Local stdio MCP server:
session, err := mcp.LoadCommand(ctx, toolReg, "uvx", []string{"mcp-server-fetch"}, mcp.LoaderOptions{})
defer session.Close()
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

go-orca integrates with MCP servers using the official [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk). On startup, go-orca calls `ListTools` on each configured MCP server and registers the discovered tools so personas can invoke them by name.

### Transport Types

| Transport | Config value | Protocol spec | When to use |
|-----------|-------------|---------------|-------------|
| Streamable HTTP | `streamable` (default) | 2025-03-26 | Modern HTTP-based MCP servers |
| SSE | `sse` | 2024-11-05 | Legacy servers using Server-Sent Events |
| Command (stdio) | `command` | any | Local subprocess MCP servers |

### Configuration

Add MCP servers to the `tools.mcp` section of your config file:

```yaml
tools:
  mcp:
    # Modern HTTP server (Streamable transport):
    - name: my-tools
      endpoint: "http://localhost:3000/mcp"
      transport: streamable      # default; can be omitted

    # Legacy SSE server:
    - name: legacy-tools
      endpoint: "http://localhost:4000/sse"
      transport: sse

    # Local subprocess (stdio):
    - name: fetch
      transport: command
      command: uvx
      args: ["mcp-server-fetch"]

    # Self-signed TLS (dev only):
    - name: dev-local
      endpoint: "https://mcp.local/mcp"
      tls_skip_verify: true
```

### Go API

```go
// HTTP server (Streamable or SSE based on LoaderOptions.Transport):
session, err := mcp.Load(ctx, toolReg, "http://localhost:3000/mcp", mcp.LoaderOptions{})
defer session.Close()

// Stdio subprocess:
session, err := mcp.LoadCommand(ctx, toolReg, "uvx", []string{"mcp-server-fetch"}, mcp.LoaderOptions{})
defer session.Close()

// In-process (testing):
serverT, clientT := sdkmcp.NewInMemoryTransports()
session, err := mcp.LoadTransport(ctx, toolReg, clientT)
defer session.Close()
```

`Load`, `LoadCommand`, and `LoadTransport` all:
1. Connect to the MCP server.
2. Call `ListTools` to discover available tools.
3. Register each discovered tool into `reg` as an `MCPTool`.
4. Return the live `*ClientSession` — the caller must keep it open while tools are in use and call `Close()` when done.

### Loader Options

| Field | Default | Description |
|-------|---------|-------------|
| `Transport` | `TransportStreamable` | HTTP transport variant (`TransportStreamable` or `TransportSSE`) |
| `HTTPTimeout` | 30s | Timeout for the initial connection and `ListTools` call |
| `TLSSkipVerify` | false | Skip TLS certificate verification — **dev only** |

### Error Handling

- If a tool's handler returns an error, the SDK sets `isError: true` on the result. go-orca surfaces this as a Go error with the tool name and the message from the first text content block.
- Connection failures during `Load` are returned immediately; the workflow does not start.
- Call timeouts are governed by the `context.Context` passed to `tool.Call`.

### Session Lifecycle

Each call to `Load` / `LoadCommand` / `LoadTransport` returns a `*ClientSession`. The session must remain open for as long as its registered tools may be called. Typical patterns:

```go
// Workflow-scoped: open before the workflow, close after.
session, err := mcp.Load(ctx, toolReg, endpoint, opts)
if err != nil { ... }
defer session.Close()
runWorkflow(ctx, toolReg)

// Application-scoped: open once at startup, close on shutdown.
appSession, _ = mcp.Load(appCtx, globalReg, endpoint, opts)
// ... server loop ...
appSession.Close()
```

---

## Registry

The global tool registry at startup:

```go
tools.Global
```

Use `toolReg.All()` to list registered tools, or `toolReg.Get("name")` to look up a specific tool.
