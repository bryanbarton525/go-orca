---
name: mcp-integration
description: How to configure, connect to, and use MCP (Model Context Protocol) servers in go-orca workflows.
---

# MCP Integration Skill

Use this skill when configuring tool access via MCP servers or when an agent needs to interact with external tools exposed over MCP.

## What Is MCP?

The **Model Context Protocol** (MCP, 2025 spec) is a JSON-RPC based protocol that lets AI models discover and call external tools through a standardized interface. go-orca supports three transport types:

| Transport | When to use |
|-----------|-------------|
| `streamable` (default) | Modern HTTP-based MCP servers (2025-03-26 spec) |
| `sse` | Legacy HTTP servers using Server-Sent Events (2024-11-05 spec) |
| `command` | Local subprocess servers run over stdio |

## Configuration

Add an MCP source to the `tools.mcp` section of your `go-orca.yaml`:

```yaml
tools:
  mcp:
    - name: fetch
      endpoint: "http://localhost:3000/mcp"
      transport: streamable   # or "sse"

    - name: local-tools
      command: uvx
      args: ["mcp-server-fetch"]
      transport: command
```

See [mcp-config.yaml](assets/mcp-config.yaml) for a complete annotated example.

## Tool Discovery

On startup go-orca calls `ListTools` on each configured MCP server and registers the discovered tools in the global tool registry. Agents can then invoke these tools by name in their `tools` list.

## Verifying Connectivity

Use the [check-mcp-server.sh](scripts/check-mcp-server.sh) script to verify that an MCP server is reachable and returns a valid tool list before adding it to configuration.

## Common Servers

See [mcp-server-catalog.md](references/mcp-server-catalog.md) for a curated list of open-source MCP servers and their endpoint conventions.

## Error Handling

- If a tool returns `isError: true`, go-orca surfaces the error message from the first text content block.
- Connection failures during `Load` are fatal for that MCP source; the workflow continues with tools from other sources.
- Use `tls_skip_verify: true` only in local/dev environments — never in production.
