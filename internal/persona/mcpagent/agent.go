// Package mcpagent runs a small-model tool loop scoped to one MCP server's tools.
// The Pod persona delegates toolchain/git/workspace work via invoke_mcp_agent
// instead of calling those MCP tools directly.
package mcpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/logger"
	"github.com/go-orca/go-orca/internal/mcp/registry"
	"github.com/go-orca/go-orca/internal/provider/common"
	"github.com/go-orca/go-orca/internal/tools"
)

// Config holds model and loop limits for MCP specialist agents.
type Config struct {
	ProviderName string
	ModelName    string
	MaxRounds    int
	Timeout      time.Duration
}

// Request is the handoff from the Pod (via invoke_mcp_agent) to a specialist.
type Request struct {
	Server        string
	Task          string
	Context       string
	WorkspacePath string
	WorkflowID    string
}

// Result is returned to the Pod as JSON from invoke_mcp_agent.
type Result struct {
	Server  string `json:"server"`
	Summary string `json:"summary"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// Run executes a bounded tool loop using only tools advertised by the named MCP server.
func Run(
	ctx context.Context,
	cfg Config,
	mcpReg *registry.Registry,
	fullReg *tools.Registry,
	req Request,
) (Result, error) {
	server := strings.TrimSpace(req.Server)
	if server == "" {
		return Result{Success: false, Error: "server is required"}, nil
	}
	task := strings.TrimSpace(req.Task)
	if task == "" {
		return Result{Success: false, Error: "task is required"}, nil
	}

	toolNames, err := mcpReg.AdvertisedTools(server)
	if err != nil {
		return Result{Server: server, Success: false, Error: err.Error()}, nil
	}
	subReg := tools.NewRegistry()
	for _, name := range toolNames {
		t, ok := fullReg.Get(name)
		if !ok {
			continue
		}
		subReg.Register(t)
	}
	if len(subReg.All()) == 0 {
		return Result{
			Server:  server,
			Success: false,
			Error:   fmt.Sprintf("no tools available for mcp server %q", server),
		}, nil
	}

	provider, ok := common.Get(cfg.ProviderName)
	if !ok {
		return Result{Server: server, Success: false, Error: fmt.Sprintf("provider %q not registered", cfg.ProviderName)}, nil
	}
	if !provider.HasCapability(common.CapabilityToolCalling) {
		return Result{Server: server, Success: false, Error: fmt.Sprintf("provider %q does not support tool calling", cfg.ProviderName)}, nil
	}

	maxRounds := cfg.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 15
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	systemPrompt := buildSystemPrompt(server, toolNames)
	userPrompt := buildUserPrompt(req)

	msgs := []common.Message{
		{Role: common.RoleSystem, Content: systemPrompt},
		{Role: common.RoleUser, Content: userPrompt},
	}
	toolDefs := specsToDefinitions(subReg.Specs())

	for round := 0; round < maxRounds; round++ {
		if runCtx.Err() != nil {
			return Result{Server: server, Success: false, Error: runCtx.Err().Error()}, nil
		}

		resp, err := provider.Chat(runCtx, common.ChatRequest{
			Model:    cfg.ModelName,
			Messages: msgs,
			Tools:    toolDefs,
		})
		if err != nil {
			return Result{Server: server, Success: false, Error: fmt.Sprintf("agent chat: %v", err)}, nil
		}
		if len(resp.Message.ToolCalls) == 0 {
			summary := strings.TrimSpace(resp.Message.Content)
			if summary == "" {
				summary = "completed (no summary from agent)"
			}
			return Result{Server: server, Summary: summary, Success: true}, nil
		}

		msgs = append(msgs, resp.Message)
		for _, tc := range resp.Message.ToolCalls {
			argsJSON, _ := json.Marshal(tc.Arguments)
			logger.Debug("mcpagent: tool call",
				zap.String("server", server),
				zap.String("workflow_id", req.WorkflowID),
				zap.String("tool", tc.Name),
			)
			toolRes := subReg.Call(runCtx, tc.Name, argsJSON)
			var content string
			if toolRes.Error != "" {
				content = fmt.Sprintf(`{"error":%q}`, toolRes.Error)
			} else {
				content = trimToolResult(toolRes.Output)
			}
			msgs = append(msgs, common.Message{
				Role:       common.RoleTool,
				Content:    content,
				ToolCallID: tc.ID,
			})
		}
	}

	return Result{
		Server:  server,
		Success: false,
		Error:   fmt.Sprintf("exceeded %d tool rounds without finishing", maxRounds),
	}, nil
}

func buildSystemPrompt(server string, toolNames []string) string {
	var b strings.Builder
	b.WriteString("You are a specialist agent for the ")
	b.WriteString(server)
	b.WriteString(" MCP server in the go-orca workflow system.\n\n")
	b.WriteString("You may ONLY use the tools exposed for this server. Execute the task using those tools.\n")
	b.WriteString("When finished, respond with a concise plain-text summary of what you did and the outcome.\n")
	b.WriteString("Do not call more tools once the task is complete.\n\n")
	b.WriteString("Available tools: ")
	b.WriteString(strings.Join(toolNames, ", "))
	b.WriteString("\n")
	return b.String()
}

func buildUserPrompt(req Request) string {
	var b strings.Builder
	if req.WorkflowID != "" {
		b.WriteString("Workflow ID: ")
		b.WriteString(req.WorkflowID)
		b.WriteString("\n")
	}
	if req.WorkspacePath != "" {
		b.WriteString("Workspace path: ")
		b.WriteString(req.WorkspacePath)
		b.WriteString("\n")
	}
	b.WriteString("\nTask:\n")
	b.WriteString(req.Task)
	if strings.TrimSpace(req.Context) != "" {
		b.WriteString("\n\nAdditional context:\n")
		b.WriteString(req.Context)
	}
	return b.String()
}

func specsToDefinitions(specs []tools.ToolSpec) []common.ToolDefinition {
	defs := make([]common.ToolDefinition, 0, len(specs))
	for _, s := range specs {
		var params map[string]interface{}
		_ = json.Unmarshal(s.Parameters, &params)
		defs = append(defs, common.ToolDefinition{
			Name:        s.Name,
			Description: s.Description,
			Parameters:  params,
		})
	}
	return defs
}

const maxToolResultBytes = 6000

func trimToolResult(raw json.RawMessage) string {
	if len(raw) <= maxToolResultBytes {
		return string(raw)
	}
	truncated := string(raw[:maxToolResultBytes])
	return truncated + "\n...(truncated)"
}
