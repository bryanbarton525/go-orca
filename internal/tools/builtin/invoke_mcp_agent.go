package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-orca/go-orca/internal/mcp/registry"
	"github.com/go-orca/go-orca/internal/persona/mcpagent"
	"github.com/go-orca/go-orca/internal/tools"
)

// InvokeMCPAgentDeps wires the invoke_mcp_agent tool to MCP registry and agent config.
type InvokeMCPAgentDeps struct {
	MCPReg   *registry.Registry
	FullReg  *tools.Registry
	AgentCfg mcpagent.Config
}

// RegisterInvokeMCPAgent registers the Pod-facing handoff tool for MCP specialists.
func RegisterInvokeMCPAgent(reg *tools.Registry, deps InvokeMCPAgentDeps) {
	if deps.MCPReg == nil || deps.FullReg == nil {
		return
	}
	reg.Register(&InvokeMCPAgentTool{deps: deps})
}

// InvokeMCPAgentTool delegates work to a per-MCP specialist agent.
type InvokeMCPAgentTool struct {
	deps InvokeMCPAgentDeps
}

var _ tools.Tool = (*InvokeMCPAgentTool)(nil)

func (t *InvokeMCPAgentTool) Name() string { return "invoke_mcp_agent" }

func (t *InvokeMCPAgentTool) Description() string {
	servers := t.deps.MCPReg.ServerNames()
	return "Delegates a task to a specialist agent that operates one MCP server's tools (e.g. git init, go build). " +
		"Use this instead of calling MCP tools directly. Servers: " + strings.Join(servers, ", ")
}

func (t *InvokeMCPAgentTool) Parameters() json.RawMessage {
	servers := t.deps.MCPReg.ServerNames()
	serverProp := map[string]any{
		"type":        "string",
		"description": "MCP server name to invoke.",
	}
	if len(servers) > 0 {
		serverProp["enum"] = servers
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"server": serverProp,
			"task": map[string]any{
				"type":        "string",
				"description": "Clear description of what the specialist should accomplish.",
			},
			"context": map[string]any{
				"type":        "string",
				"description": "Optional extra context (paths, constraints, expected outcomes).",
			},
			"workflow_id": map[string]any{
				"type":        "string",
				"description": "Workflow ID (for logging).",
			},
			"workspace_path": map[string]any{
				"type":        "string",
				"description": "Absolute workspace path the specialist should use.",
			},
		},
		"required": []string{"server", "task"},
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object","properties":{"server":{"type":"string"},"task":{"type":"string"}},"required":["server","task"]}`)
	}
	return b
}

type invokeMCPAgentArgs struct {
	Server        string `json:"server"`
	Task          string `json:"task"`
	Context       string `json:"context,omitempty"`
	WorkflowID    string `json:"workflow_id,omitempty"`
	WorkspacePath string `json:"workspace_path,omitempty"`
}

func (t *InvokeMCPAgentTool) Call(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a invokeMCPAgentArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invoke_mcp_agent: invalid args: %w", err)
	}

	res, err := mcpagent.Run(ctx, t.deps.AgentCfg, t.deps.MCPReg, t.deps.FullReg, mcpagent.Request{
		Server:        a.Server,
		Task:          a.Task,
		Context:       a.Context,
		WorkflowID:    a.WorkflowID,
		WorkspacePath: a.WorkspacePath,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(res)
}
