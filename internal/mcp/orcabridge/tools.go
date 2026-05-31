package orcabridge

import (
	"context"
	"encoding/json"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/go-orca/go-orca/internal/mcp/server"
)

// BridgeArgs carries optional fields for all bridge tools (schema is permissive).
type BridgeArgs struct {
	Request         string `json:"request,omitempty"`
	Title           string `json:"title,omitempty"`
	Mode            string `json:"mode,omitempty"`
	Provider        string `json:"provider,omitempty"`
	Model           string `json:"model,omitempty"`
	TemplateID      string `json:"template_id,omitempty"`
	AutoMode        bool   `json:"auto_mode,omitempty"`
	WorkflowID      string `json:"workflow_id,omitempty"`
	Persona         string `json:"persona,omitempty"`
	Persist         bool   `json:"persist,omitempty"`
	ToolsScope      string `json:"tools_scope,omitempty"`
	TimeoutSec      int    `json:"timeout_sec,omitempty"`
	WorkspacePath   string `json:"workspace_path,omitempty"`
	TenantID        string `json:"tenant_id,omitempty"`
	ScopeID         string `json:"scope_id,omitempty"`
	TaskTitle       string `json:"task_title,omitempty"`
	TaskDescription string `json:"task_description,omitempty"`
	TaskSpecialty   string `json:"task_specialty,omitempty"`
	TaskTier        string `json:"task_tier,omitempty"`
}

// RegisterTools wires bridge tools onto an MCP server.
func RegisterTools(s *server.Server, client *Client) {
	type handler func(context.Context, BridgeArgs) (*sdkmcp.CallToolResult, error)

	register := func(name, desc string, fn handler) {
		tool := &sdkmcp.Tool{Name: name, Description: desc}
		sdkmcp.AddTool(s.MCPServer(), tool, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args BridgeArgs) (*sdkmcp.CallToolResult, any, error) {
			res, err := fn(ctx, args)
			return res, nil, err
		})
	}

	register("orca_workflow_create", "Create a full go-orca workflow and enqueue execution.", func(ctx context.Context, a BridgeArgs) (*sdkmcp.CallToolResult, error) {
		var out map[string]any
		err := client.do(ctx, "POST", "/api/v1/workflows", map[string]any{
			"request": a.Request, "title": a.Title, "mode": a.Mode,
			"provider": a.Provider, "model": a.Model,
			"template_id": a.TemplateID, "auto_mode": a.AutoMode,
		}, a.TenantID, a.ScopeID, &out)
		return textResult(out, err)
	})

	register("orca_workflow_status", "Get workflow status.", func(ctx context.Context, a BridgeArgs) (*sdkmcp.CallToolResult, error) {
		var out map[string]any
		err := client.do(ctx, "GET", "/api/v1/workflows/"+a.WorkflowID, nil, a.TenantID, a.ScopeID, &out)
		return textResult(out, err)
	})

	register("orca_workflow_events", "List workflow events.", func(ctx context.Context, a BridgeArgs) (*sdkmcp.CallToolResult, error) {
		var out map[string]any
		err := client.do(ctx, "GET", "/api/v1/workflows/"+a.WorkflowID+"/events", nil, a.TenantID, a.ScopeID, &out)
		return textResult(out, err)
	})

	register("orca_workflow_cancel", "Cancel workflow.", func(ctx context.Context, a BridgeArgs) (*sdkmcp.CallToolResult, error) {
		var out map[string]any
		err := client.do(ctx, "POST", "/api/v1/workflows/"+a.WorkflowID+"/cancel", nil, a.TenantID, a.ScopeID, &out)
		return textResult(out, err)
	})

	register("orca_workflow_resume", "Resume workflow.", func(ctx context.Context, a BridgeArgs) (*sdkmcp.CallToolResult, error) {
		var out map[string]any
		err := client.do(ctx, "POST", "/api/v1/workflows/"+a.WorkflowID+"/resume", nil, a.TenantID, a.ScopeID, &out)
		return textResult(out, err)
	})

	register("orca_persona_run", "Run one persona ad-hoc.", func(ctx context.Context, a BridgeArgs) (*sdkmcp.CallToolResult, error) {
		body := map[string]any{
			"persona": a.Persona, "request": a.Request, "mode": a.Mode,
			"provider": a.Provider, "model": a.Model, "workflow_id": a.WorkflowID,
			"persist": a.Persist, "tools_scope": a.ToolsScope,
			"timeout_sec": a.TimeoutSec, "workspace_path": a.WorkspacePath,
		}
		if a.TaskTitle != "" || a.TaskDescription != "" {
			body["task_run"] = map[string]any{
				"title": a.TaskTitle, "description": a.TaskDescription,
				"specialty": a.TaskSpecialty, "tier": a.TaskTier,
			}
		}
		var out map[string]any
		err := client.do(ctx, "POST", "/api/v1/persona-runs", body, a.TenantID, a.ScopeID, &out)
		return textResult(out, err)
	})

	register("orca_task_run", "Run Pod/QA on one task.", func(ctx context.Context, a BridgeArgs) (*sdkmcp.CallToolResult, error) {
		p := a.Persona
		if p == "" {
			p = "pod"
		}
		body := map[string]any{
			"persona": p, "request": a.Request, "tools_scope": a.ToolsScope,
			"task_run": map[string]any{
				"title": a.TaskTitle, "description": a.TaskDescription,
				"specialty": a.TaskSpecialty, "tier": a.TaskTier,
			},
		}
		var out map[string]any
		err := client.do(ctx, "POST", "/api/v1/persona-runs", body, a.TenantID, a.ScopeID, &out)
		return textResult(out, err)
	})
}

func textResult(v any, err error) (*sdkmcp.CallToolResult, error) {
	if err != nil {
		return &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: err.Error()}},
			IsError: true,
		}, nil
	}
	raw, _ := json.MarshalIndent(v, "", "  ")
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(raw)}},
	}, nil
}
