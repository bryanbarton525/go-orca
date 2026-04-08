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
		JSONMode: true,
	})
	if err != nil {
		return "", fmt.Errorf("executor: chat error: %w", err)
	}

	if resp.Truncated {
		return resp.Message.Content, fmt.Errorf(
			"executor: model %q truncated its response after %d output tokens (hit token limit) — "+
				"try a model with a larger context window or simplify the request",
			resp.Model, resp.OutputTokens,
		)
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
// When the model truncated its output (hit token limit), it attempts to close
// open JSON structures and retry — and always reports truncation clearly.
func ParseJSON(raw string, target interface{}) error {
	cleaned := extractJSON(raw)

	if err := json.Unmarshal([]byte(cleaned), target); err != nil {
		// Detect truncation: json.Unmarshal reports "unexpected end of JSON input"
		// when the JSON stream was cut off. Try to recover by closing open brackets.
		if strings.Contains(err.Error(), "unexpected end of JSON") {
			if recovered := repairTruncatedJSON(cleaned); recovered != cleaned {
				if err2 := json.Unmarshal([]byte(recovered), target); err2 == nil {
					// Parsed after repair — warn but don't fail.
					return fmt.Errorf("base: model output was truncated (hit token limit) — JSON was partially repaired; some fields may be missing")
				}
			}
			return fmt.Errorf(
				"base: model output was truncated (hit token limit) — response ended mid-JSON and could not be repaired\n"+
					"Hint: use a model with a larger context window, or reduce the complexity of the request\n"+
					"Output length: %d chars\nRaw tail: ...%s",
				len(raw), tail(raw, 120),
			)
		}
		return fmt.Errorf("base: could not parse model response as JSON: %w\nRaw content:\n%s", err, raw)
	}
	return nil
}

// repairTruncatedJSON attempts to close any unclosed JSON objects and arrays
// so that a partially-generated response can still be unmarshalled.
func repairTruncatedJSON(s string) string {
	var stack []rune
	inString := false
	escaped := false

	for _, ch := range s {
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	if len(stack) == 0 {
		return s // nothing to close
	}

	// Trim trailing garbage (partial key/value) and close.
	// Find the last complete value boundary: the last } ] " digit.
	b := []byte(s)
	for i := len(b) - 1; i >= 0; i-- {
		c := b[i]
		if c == '}' || c == ']' || c == '"' || (c >= '0' && c <= '9') || c == 'e' || c == 'l' || c == 's' {
			b = b[:i+1]
			break
		}
	}

	// Close open structures in reverse order.
	for i := len(stack) - 1; i >= 0; i-- {
		b = append(b, byte(stack[i]))
	}
	return string(b)
}

// tail returns the last n characters of s.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
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
