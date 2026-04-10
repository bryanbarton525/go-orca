# MCP Server Catalog

Curated list of open-source MCP servers with endpoint conventions.

## Official / Reference Servers

| Server | Transport | Endpoint convention | Notes |
|--------|-----------|---------------------|-------|
| `@modelcontextprotocol/server-fetch` | command | `uvx mcp-server-fetch` | Fetches web pages as text |
| `@modelcontextprotocol/server-filesystem` | command | `uvx mcp-server-filesystem /path` | Read / write files in a sandboxed directory |
| `@modelcontextprotocol/server-memory` | command | `uvx mcp-server-memory` | Persistent entity + relation store |
| `@modelcontextprotocol/server-github` | command | `uvx mcp-server-github` | GitHub API tools (PRs, issues, files) |
| `@modelcontextprotocol/server-postgres` | command | `uvx mcp-server-postgres $DATABASE_URL` | SQL query tool |

## Community Servers

| Server | Transport | Notes |
|--------|-----------|-------|
| `mcp-server-brave-search` | command | Web search via Brave API |
| `mcp-server-puppeteer` | command | Browser automation |
| `mcp-server-slack` | command | Post messages, read channels |

## Self-Hosted HTTP Servers

For Streamable HTTP MCP servers, the endpoint is typically:

```
http://<host>:<port>/mcp
```

For legacy SSE servers:

```
http://<host>:<port>/sse
```

Always verify with `scripts/check-mcp-server.sh` before adding to config.
