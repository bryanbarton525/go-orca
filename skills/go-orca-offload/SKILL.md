---
name: go-orca-offload
description: Offload expensive agent work to a go-orca cluster via the mcp-orca MCP bridge (workflows and ad-hoc persona runs).
---

# go-orca Offload Skill

Use when a **local** agent (Cursor, VS Code, Claude Desktop) should delegate implementation, QA, or full pipelines to a **go-orca** cluster.

## MCP server setup

Point your MCP client at the cluster bridge:

```json
{
  "mcpServers": {
    "go-orca": {
      "url": "http://mcp-orca:3000/mcp",
      "headers": {
        "Authorization": "Bearer <zitadel-or-authentik-access-token-or-pat>"
      }
    }
  }
}
```

On the bridge pod, set `GOORCA_MCP_API_KEY` to the same Bearer token (proxied to `go-orca-api`), or rely on MCP client headers when your client forwards them.

Set `GOORCA_TENANT_ID` and `GOORCA_SCOPE_ID` on the bridge pod, or pass `tenant_id` / `scope_id` per tool call.

### API OIDC (go-orca-api)

```yaml
server:
  oidc:
    userinfo_url: "https://auth.example.com/oidc/v1/userinfo"   # Zitadel
    required: true
```

Authentik: `https://<host>/application/o/<slug>/userinfo/`

## When to use which tool

| Tool | Use when |
|------|----------|
| `orca_workflow_create` | Full pipeline: PM → Architect → Pod → QA → Finalizer |
| `orca_workflow_status` / `orca_workflow_events` | Poll progress |
| `orca_persona_run` | One persona, one shot (no full workflow) |
| `orca_task_run` | Single Pod or QA task (convenience wrapper) |

Read MCP prompts: `offload-workflow`, `offload-persona`, `orca-capabilities`.

## Templates

Pass `template_id` on workflow create, e.g. `software-default`, `content-default`, `ops-default`. Templates pin `required_personas`, models, and pod specialties.

## Tools scope (persona runs)

- `full` — full tool registry on Pod
- `mcp_agent` — `invoke_mcp_agent` only (auxiliary / small model; good for MCP toolchain calls)
- `none` — no tools

## Provider tiers

Homelab may run **ollama-gpu** (primary) and **ollama-cpu** (auxiliary). PM and MCP-agent work can route to CPU when GPU is saturated. See [workflow-orchestration](../workflow-orchestration/SKILL.md) for persona model rules.

## API (without MCP)

- `POST /api/v1/workflows` — full workflow
- `POST /api/v1/persona-runs` — ad-hoc persona
- `GET /api/v1/workflow-templates` — template catalog

Requires `X-Tenant-ID`, `X-Scope-ID`, and `Authorization: Bearer` when `server.oidc.required` is true (validated via userinfo).
