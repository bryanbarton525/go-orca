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
	// SchemaName is a short identifier for the output schema (e.g. "director_output").
	// Required by providers that need a named schema (e.g. OpenAI json_schema mode).
	SchemaName string
	// OutputSchema is the JSON Schema that constrains the model's response.
	// When set, the provider uses its strongest structured-output mechanism.
	// When nil, JSONMode (format=json) is used as a fallback.
	OutputSchema map[string]any
}

// NewExecutor creates an Executor with the given system prompt and optional
// output schema. Pass a nil schema to fall back to plain JSON mode.
func NewExecutor(systemPrompt string, schemaName string, schema map[string]any) Executor {
	return Executor{SystemPrompt: systemPrompt, SchemaName: schemaName, OutputSchema: schema}
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
		Model:        packet.ModelName,
		Messages:     msgs,
		JSONMode:     true,
		OutputSchema: e.OutputSchema,
		SchemaName:   e.SchemaName,
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
					// Repaired successfully — log and continue; don't fail the task.
					return nil
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

// extractJSON strips markdown code fences from a model response, then finds
// the first { or [ if there is leading prose before the JSON.
//
// It only looks for code fences when the response actually starts with a
// backtick — NOT when the first character is already { or [. This avoids
// misidentifying Go code fences embedded inside JSON string values (e.g.
// inside the "content" field of an implementer artifact) as fence wrappers.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)

	if strings.HasPrefix(s, "```") {
		// Response is wrapped in a markdown code fence — strip it.
		for _, fence := range []string{"```json", "```"} {
			if strings.HasPrefix(s, fence) {
				s = s[len(fence):]
				if end := strings.LastIndex(s, "```"); end != -1 {
					s = s[:end]
				}
				return strings.TrimSpace(s)
			}
		}
	}

	// No fence wrapper. The response may have prose before a fenced JSON block,
	// e.g. "Here is the output:\n```json\n{...}\n```". In that case, strip the
	// intervening fence opener and then trim whatever closing fence remains.
	if idx := strings.Index(s, "```"); idx != -1 {
		after := strings.TrimSpace(s[idx:])
		for _, fence := range []string{"```json", "```"} {
			if strings.HasPrefix(after, fence) {
				after = strings.TrimSpace(after[len(fence):])
				break
			}
		}
		// Strip trailing fence, if any.
		if end := strings.LastIndex(after, "```"); end != -1 {
			after = strings.TrimSpace(after[:end])
		}
		// Make sure we actually found JSON, not just more prose.
		if len(after) > 0 && (after[0] == '{' || after[0] == '[') {
			return after
		}
	}

	// Advance past any leading prose to the first { or [.
	for i, ch := range s {
		if ch == '{' || ch == '[' {
			return s[i:]
		}
	}
	return s
}

// Timestamp returns current UTC time.
func Timestamp() time.Time { return time.Now().UTC() }
