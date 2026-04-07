// Package base provides a shared executor used by all built-in personas.
// It handles provider resolution, prompt construction from templates and
// skill/agent overlays, and response parsing.
package base

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-orca/go-orca/internal/provider/common"
	"github.com/go-orca/go-orca/internal/state"
)

// Executor is the shared execution engine embedded in every built-in persona.
type Executor struct {
	SystemPrompt string
}

// NewExecutor creates an Executor with the given system prompt.
func NewExecutor(systemPrompt string) Executor {
	return Executor{SystemPrompt: systemPrompt}
}

// Run sends a chat request to the provider resolved from the HandoffPacket,
// returns the raw assistant response text.
func (e *Executor) Run(ctx context.Context, packet state.HandoffPacket, userPrompt string) (string, error) {
	provider, ok := common.Get(packet.ProviderName)
	if !ok {
		return "", fmt.Errorf("executor: provider %q not registered", packet.ProviderName)
	}

	systemContent := e.buildSystemContent(packet)

	msgs := []common.Message{
		{Role: common.RoleSystem, Content: systemContent},
		{Role: common.RoleUser, Content: userPrompt},
	}

	resp, err := provider.Chat(ctx, common.ChatRequest{
		Model:    packet.ModelName,
		Messages: msgs,
	})
	if err != nil {
		return "", fmt.Errorf("executor: chat error: %w", err)
	}

	return resp.Message.Content, nil
}

// buildSystemContent layers the base system prompt with any skill/agent
// overlays baked into the HandoffPacket.
func (e *Executor) buildSystemContent(packet state.HandoffPacket) string {
	var sb strings.Builder
	sb.WriteString(e.SystemPrompt)

	if packet.CustomAgentMD != "" {
		sb.WriteString("\n\n---\n## Agent instructions\n")
		sb.WriteString(packet.CustomAgentMD)
	}
	if packet.SkillsContext != "" {
		sb.WriteString("\n\n---\n## Available skills\n")
		sb.WriteString(packet.SkillsContext)
	}
	if packet.PromptsContext != "" {
		sb.WriteString("\n\n---\n## Additional prompts\n")
		sb.WriteString(packet.PromptsContext)
	}

	return sb.String()
}

// BuildHandoffContext builds a concise text summary of the workflow state
// to include in user prompts so the model has full context.
func BuildHandoffContext(packet state.HandoffPacket) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Workflow\nID: %s\nMode: %s\nRequest: %s\n",
		packet.WorkflowID, packet.Mode, packet.Request))

	if len(packet.Summaries) > 0 {
		sb.WriteString("\n## Prior Phase Summaries\n")
		order := []state.PersonaKind{
			state.PersonaDirector, state.PersonaProjectMgr,
			state.PersonaArchitect, state.PersonaImplementer,
			state.PersonaQA,
		}
		for _, k := range order {
			if s, ok := packet.Summaries[k]; ok && s != "" {
				sb.WriteString(fmt.Sprintf("### %s\n%s\n", k, s))
			}
		}
	}

	if packet.Constitution != nil {
		if b, err := json.MarshalIndent(packet.Constitution, "", "  "); err == nil {
			sb.WriteString("\n## Constitution\n```json\n")
			sb.Write(b)
			sb.WriteString("\n```\n")
		}
	}

	if packet.Requirements != nil {
		if b, err := json.MarshalIndent(packet.Requirements, "", "  "); err == nil {
			sb.WriteString("\n## Requirements\n```json\n")
			sb.Write(b)
			sb.WriteString("\n```\n")
		}
	}

	if packet.Design != nil {
		if b, err := json.MarshalIndent(packet.Design, "", "  "); err == nil {
			sb.WriteString("\n## Design\n```json\n")
			sb.Write(b)
			sb.WriteString("\n```\n")
		}
	}

	if len(packet.Tasks) > 0 {
		sb.WriteString("\n## Tasks\n")
		for _, t := range packet.Tasks {
			sb.WriteString(fmt.Sprintf("- [%s] %s: %s\n", t.Status, t.ID[:8], t.Title))
		}
	}

	return sb.String()
}

// ParseJSON attempts to unmarshal the model's response into the target struct.
// It handles responses that wrap JSON in markdown code fences.
func ParseJSON(raw string, target interface{}) error {
	cleaned := extractJSON(raw)
	if err := json.Unmarshal([]byte(cleaned), target); err != nil {
		return fmt.Errorf("base: JSON parse error: %w\nRaw content:\n%s", err, raw)
	}
	return nil
}

// extractJSON strips markdown code fences from a model response.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	for _, fence := range []string{"```json", "```"} {
		if idx := strings.Index(s, fence); idx != -1 {
			s = s[idx+len(fence):]
			if end := strings.LastIndex(s, "```"); end != -1 {
				s = s[:end]
			}
			return strings.TrimSpace(s)
		}
	}
	return s
}

// Timestamp returns current UTC time.
func Timestamp() time.Time { return time.Now().UTC() }
