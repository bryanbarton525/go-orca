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

	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/logger"
	"github.com/go-orca/go-orca/internal/provider/common"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/tools"
)

// Executor is the shared execution engine embedded in every built-in persona.
type Executor struct {
	// SchemaName is a short identifier for the output schema (e.g. "director_output").
	// Required by providers that need a named schema (e.g. OpenAI json_schema mode).
	SchemaName string
	// OutputSchema is the JSON Schema that constrains the model's response.
	// When set, the provider uses its strongest structured-output mechanism.
	// When nil, JSONMode (format=json) is used as a fallback.
	OutputSchema map[string]any
}

// NewExecutor creates an Executor with the given schema name and optional
// output schema. The system prompt is no longer stored at construction time;
// it is passed per-call via Run so that it can come from the persisted
// workflow-scoped prompt snapshot. Pass a nil schema to fall back to plain
// JSON mode.
func NewExecutor(schemaName string, schema map[string]any) Executor {
	return Executor{SchemaName: schemaName, OutputSchema: schema}
}

// Run sends a chat request to the provider resolved from the HandoffPacket,
// returns the raw assistant response text.
//
// systemPrompt is the base persona system prompt loaded from the workflow's
// PersonaPromptSnapshot. It is layered with customization overlays from the
// packet before being sent to the model.
//
// If the packet carries a ToolRegistry and the provider supports tool-calling,
// Run first runs a tool-call loop (Phase A) to collect live tool results, then
// makes a final structured-JSON call (Phase B) with that enriched context.
func (e *Executor) Run(ctx context.Context, packet state.HandoffPacket, systemPrompt, userPrompt string) (string, error) {
	provider, ok := common.Get(packet.ProviderName)
	if !ok {
		return "", fmt.Errorf("executor: provider %q not registered", packet.ProviderName)
	}

	// Phase A uses the full system content including tool descriptions.
	// Phase B replaces the system message with a tools-stripped variant to
	// prevent the model from emitting freeform tool-call prose instead of JSON.
	phaseASystem := e.buildSystemContent(systemPrompt, packet, true)
	phaseBSystem := e.buildSystemContent(systemPrompt, packet, false)

	msgs := []common.Message{
		{Role: common.RoleSystem, Content: phaseASystem},
		{Role: common.RoleUser, Content: userPrompt},
	}

	// ── Phase A: tool-call loop ───────────────────────────────────────────────
	// If tools are available and the provider claims tool-calling capability,
	// give the model up to maxToolRounds to gather live context before the
	// final structured response.
	if packet.ToolRegistry != nil && len(packet.ToolRegistry.All()) > 0 &&
		provider.HasCapability(common.CapabilityToolCalling) {

		toolDefs := specsToDefinitions(packet.ToolRegistry.Specs())
		const maxToolRounds = 5
		for range maxToolRounds {
			toolResp, err := provider.Chat(ctx, common.ChatRequest{
				Model:    packet.ModelName,
				Messages: msgs,
				Tools:    toolDefs,
			})
			if err != nil {
				return "", fmt.Errorf("executor: tool-call chat error: %w", err)
			}
			if len(toolResp.Message.ToolCalls) == 0 {
				// Model chose not to call any tools — skip to Phase B.
				break
			}

			// Append the assistant turn that requested the tool calls.
			msgs = append(msgs, toolResp.Message)

			// Execute each requested tool and append its result.
			for _, tc := range toolResp.Message.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Arguments)
				logger.Debug("executor: dispatching tool call",
					zap.String("workflow_id", packet.WorkflowID),
					zap.String("persona", string(packet.CurrentPersona)),
					zap.String("tool", tc.Name),
					zap.String("args", string(argsJSON)),
				)
				result := packet.ToolRegistry.Call(ctx, tc.Name, argsJSON)
				var content string
				if result.Error != "" {
					logger.Warn("executor: tool call error",
						zap.String("tool", tc.Name),
						zap.String("error", result.Error),
					)
					content = fmt.Sprintf(`{"error":%q}`, result.Error)
				} else {
					logger.Debug("executor: tool call succeeded",
						zap.String("tool", tc.Name),
						zap.Int("result_bytes", len(result.Output)),
					)
					content = trimToolResult(result.Output)
				}
				msgs = append(msgs, common.Message{
					Role:       common.RoleTool,
					Content:    content,
					ToolCallID: tc.ID,
				})
			}
		}
	}

	// ── Phase B: structured JSON response ────────────────────────────────────
	// Replace the system message with one that strips tool descriptions — the
	// model must not emit freeform tool-call prose here. Then append an explicit
	// JSON-only instruction as the final user turn so that providers which do not
	// enforce output schemas (e.g. Copilot) still receive a strong text-level
	// signal to produce only JSON.
	phaseBMsgs := make([]common.Message, 0, len(msgs)+1)
	for _, m := range msgs {
		if m.Role == common.RoleSystem {
			phaseBMsgs = append(phaseBMsgs, common.Message{Role: common.RoleSystem, Content: phaseBSystem})
		} else {
			phaseBMsgs = append(phaseBMsgs, m)
		}
	}
	phaseBMsgs = append(phaseBMsgs, common.Message{
		Role: common.RoleUser,
		Content: "Respond with ONLY a valid JSON object matching the required output schema. " +
			"Do not include any prose, markdown fences, tool calls, or commentary outside the JSON object.",
	})

	resp, err := provider.Chat(ctx, common.ChatRequest{
		Model:        packet.ModelName,
		Messages:     phaseBMsgs,
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

// specsToDefinitions converts tool specs from the registry into the canonical
// ToolDefinition type the provider Chat API accepts.
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

// maxToolResultBytes is the maximum number of bytes kept from a single tool
// result before it is truncated. Context window budget on small local models
// is tight; Context7 and HTTP responses can easily exceed 10 KB per call.
const maxToolResultBytes = 6000

// trimToolResult ensures a tool result fits within the context window budget.
// If the raw bytes exceed maxToolResultBytes the content is truncated and a
// notice appended so the model knows the response was cut short.
func trimToolResult(raw []byte) string {
	if len(raw) <= maxToolResultBytes {
		return string(raw)
	}
	return string(raw[:maxToolResultBytes]) +
		fmt.Sprintf("\n\n[... truncated: %d bytes omitted to stay within context budget ...]", len(raw)-maxToolResultBytes)
}

// buildSystemContent layers the base system prompt with any skill/agent
// overlays baked into the HandoffPacket.
//
// includeTools controls whether ToolsContext is appended. Pass true for Phase A
// (tool-call loop) so the model knows what tools are available. Pass false for
// Phase B (structured JSON output) so the model cannot hallucinate freeform
// tool-call prose instead of the required JSON artifact.
func (e *Executor) buildSystemContent(systemPrompt string, packet state.HandoffPacket, includeTools bool) string {
	var sb strings.Builder
	sb.WriteString(systemPrompt)

	if packet.CustomAgentMD != "" {
		sb.WriteString("\n\n---\n## Agent instructions\n")
		sb.WriteString(packet.CustomAgentMD)
	}
	if packet.SkillsContext != "" {
		sb.WriteString("\n\n---\n## Available skills\n")
		sb.WriteString(packet.SkillsContext)
	}
	if includeTools && packet.ToolsContext != "" {
		sb.WriteString("\n\n---\n## Available tools\n")
		sb.WriteString(packet.ToolsContext)
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

	// Surface blocking issues so Implementer (on remediation) and Architect
	// know exactly what QA rejected.  This is critical context that was missing
	// previously, causing the Implementer to retry without knowing what to fix.
	if len(packet.BlockingIssues) > 0 {
		sb.WriteString("\n## QA Blocking Issues\nThe following issues were raised by QA and MUST be resolved:\n")
		for _, issue := range packet.BlockingIssues {
			sb.WriteString(fmt.Sprintf("- %s\n", issue))
		}
	}

	if packet.IsRemediation {
		sb.WriteString(fmt.Sprintf("\n## Remediation Context\nThis is a targeted remediation pass (QA cycle %d). ", packet.QACycle))
		sb.WriteString("The blocking issues listed above were found in the previous QA review. ")
		sb.WriteString("Produce ONLY the specific implementer tasks needed to resolve those issues. ")
		sb.WriteString("Do NOT re-plan the entire project.\n")
	}

	return sb.String()
}

// ParseJSON attempts to unmarshal the model's response into the target struct.
// It handles responses that wrap JSON in markdown code fences.
// When the model truncated its output (hit token limit), it attempts to close
// open JSON structures and retry — and always reports truncation clearly.
func ParseJSON(raw string, target interface{}) error {
	// Try the last complete JSON object first. Models that interleave tool-call
	// prose with a final artifact always emit the artifact last. Picking the last
	// balanced {...} block avoids mistaking an earlier small object (e.g.
	// {"path":"..."}) for the artifact, which would fail with "invalid character
	// 't' after top-level value" when the trailing text follows it.
	if last := lastJSONObject(raw); last != "" {
		if err := json.Unmarshal([]byte(last), target); err == nil {
			return nil
		}
	}

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

// lastJSONObject returns the last complete, balanced JSON object ({...}) found
// in s. It tracks JSON string boundaries so that { and } inside string values
// are not counted as depth. Returns "" if no complete object is found.
func lastJSONObject(s string) string {
	var result string
	i := 0
	for i < len(s) {
		if s[i] != '{' {
			i++
			continue
		}
		end := findMatchingClose(s, i)
		if end < 0 {
			i++
			continue
		}
		result = s[i : end+1]
		i = end + 1
	}
	return result
}

// findMatchingClose returns the index of the } that closes the { at position
// start. It tracks JSON string boundaries (including \ escapes) so that braces
// inside string values are not counted. Returns -1 if not found.
func findMatchingClose(s string, start int) int {
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		b := s[i]
		if escaped {
			escaped = false
			continue
		}
		if b == '\\' && inString {
			escaped = true
			continue
		}
		if b == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch b {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
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
	// For {, verify the next non-whitespace character is " or } — the only
	// valid starts for a JSON object. This skips false positives such as
	// "{Jsii" that appear in garbled model output before the real payload.
	for i, ch := range s {
		if ch == '{' {
			rest := strings.TrimSpace(s[i+1:])
			if len(rest) > 0 && rest[0] != '"' && rest[0] != '}' {
				continue
			}
			return s[i:]
		}
		if ch == '[' {
			return s[i:]
		}
	}
	return s
}

// Timestamp returns current UTC time.
func Timestamp() time.Time { return time.Now().UTC() }
