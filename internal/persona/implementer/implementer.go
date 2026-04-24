// Package implementer implements the Implementer persona, responsible for
// executing individual tasks from the task graph and producing artifacts.
package implementer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/persona/prompts"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/google/uuid"
)

// implOutput is the expected JSON shape from the Implementer.
type implOutput struct {
	ArtifactKind        string   `json:"artifact_kind"`
	ArtifactName        string   `json:"artifact_name"`
	ArtifactDescription string   `json:"artifact_description"`
	Content             string   `json:"content"`
	Summary             string   `json:"summary"`
	Issues              []string `json:"issues"`
}

// outputSchema defines the structured JSON shape for Implementer responses.
var outputSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"artifact_kind":        map[string]any{"type": "string"},
		"artifact_name":        map[string]any{"type": "string"},
		"artifact_description": map[string]any{"type": "string"},
		"content":              map[string]any{"type": "string"},
		"summary":              map[string]any{"type": "string"},
		"issues":               map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	},
	"required": []string{"artifact_kind", "artifact_name", "artifact_description", "content", "summary", "issues"},
}

// Implementer implements persona.Persona.
type Implementer struct {
	exec base.Executor
}

// New returns a new Implementer persona.
func New() *Implementer {
	return &Implementer{exec: base.NewExecutor("implementer_output", outputSchema)}
}

// Kind implements Persona.
func (im *Implementer) Kind() state.PersonaKind { return state.PersonaImplementer }

// Name implements Persona.
func (im *Implementer) Name() string { return "Implementer" }

// Description implements Persona.
func (im *Implementer) Description() string {
	return "Executes tasks from the task graph and produces typed artifacts."
}

// Execute implements Persona.
//
// The Implementer runs once per ready task.  The engine is responsible for
// calling Execute repeatedly until all implementer tasks are complete.
// The HandoffPacket.Tasks slice should contain the single task being executed.
func (im *Implementer) Execute(ctx context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error) {
	_ = time.Now()

	if len(packet.Tasks) == 0 {
		return nil, fmt.Errorf("implementer: no task in handoff packet")
	}
	task := packet.Tasks[0]

	systemPrompt := packet.PersonaPromptSnapshot[prompts.KeyImplementer]

	ctx_ := buildContext(packet)
	userPrompt := fmt.Sprintf(
		"%s\n\n## Current Task\nTitle: %s\nDescription: %s\n\nImplement this task and produce your JSON output.",
		ctx_, task.Title, task.Description,
	)

	raw, err := im.exec.Run(ctx, packet, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("implementer: execution error: %w", err)
	}

	var out implOutput
	if err := base.ParseJSON(raw, &out); err != nil {
		return nil, fmt.Errorf("implementer: parse error: %w", err)
	}

	// Guard against an empty content field.  An artifact with no content is
	// worse than a failed task because it silently reaches QA, which then
	// raises a "No artifact provided" blocking issue that confuses the
	// remediation loop.  Surface the failure here instead.
	if strings.TrimSpace(out.Content) == "" {
		return nil, fmt.Errorf("implementer: model produced an empty content field for task %q — check model output or prompt", task.Title)
	}

	now := base.Timestamp()
	artifact := state.Artifact{
		ID:          uuid.New().String(),
		WorkflowID:  packet.WorkflowID,
		TaskID:      task.ID,
		Kind:        normalizeArtifactKind(packet.Mode, out.ArtifactKind),
		Name:        out.ArtifactName,
		Description: out.ArtifactDescription,
		Content:     out.Content,
		CreatedBy:   state.PersonaImplementer,
		CreatedAt:   now,
	}

	return &state.PersonaOutput{
		Persona:     state.PersonaImplementer,
		Summary:     out.Summary,
		RawContent:  raw,
		Artifacts:   []state.Artifact{artifact},
		Suggestions: out.Issues,
		CompletedAt: now,
	}, nil
}

func normalizeArtifactKind(mode state.WorkflowMode, raw string) state.ArtifactKind {
	kind := state.ArtifactKind(strings.TrimSpace(strings.ToLower(raw)))
	switch kind {
	case state.ArtifactKindCode,
		state.ArtifactKindDocument,
		state.ArtifactKindDiagram,
		state.ArtifactKindMarkdown,
		state.ArtifactKindConfig,
		state.ArtifactKindReport,
		state.ArtifactKindBlogPost,
		state.ArtifactKindBundleRef:
		return kind
	case "blog", "blog-post", "blogpost", "post", "article":
		return state.ArtifactKindBlogPost
	case "plain_text", "plaintext", "text", "txt":
		if mode == state.WorkflowModeContent {
			return state.ArtifactKindBlogPost
		}
		return state.ArtifactKindDocument
	case "doc", "docs":
		return state.ArtifactKindDocument
	}

	if mode == state.WorkflowModeContent {
		return state.ArtifactKindBlogPost
	}
	return state.ArtifactKindDocument
}

// maxSourceArtifactChars is the maximum number of characters of a source
// artifact (e.g. the current blog post) included in the implementer's context.
// Content beyond this limit is truncated; the truncation notice tells the model
// the document continues so it does not hallucinate missing sections.
const maxSourceArtifactChars = 8000

// buildContext constructs a focused prompt context for the implementer.
//
// Unlike the shared BuildHandoffContext used by planning personas, this omits
// the accumulated persona summaries, Requirements JSON, and Design JSON — none
// of which help the implementer execute a single atomic task.  That material
// was already distilled into the task descriptions by the Architect.
//
// For content-mode workflows we additionally inject the most recent synthesis
// artifact (blog_post or, absent that, markdown) as source material.  Without
// this, a synthesis or fix task forces the model to regenerate the entire
// document from memory rather than patching the existing one.
func buildContext(packet state.HandoffPacket) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Workflow\nMode: %s\nRequest: %s\n",
		packet.Mode, packet.Request))

	if len(packet.Tasks) > 0 {
		sb.WriteString("\n## Task Plan\n")
		for _, t := range packet.Tasks {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", t.Status, t.Title))
		}
	}

	if len(packet.BlockingIssues) > 0 {
		sb.WriteString("\n## QA Blocking Issues\nThe following issues were raised by QA and MUST be resolved:\n")
		for _, issue := range packet.BlockingIssues {
			sb.WriteString(fmt.Sprintf("- %s\n", issue))
		}
	}

	if packet.IsRemediation {
		sb.WriteString(fmt.Sprintf(
			"\n## Remediation Context\nThis is a targeted remediation pass (QA cycle %d). "+
				"Resolve ONLY the blocking issues listed above. Do NOT re-plan the entire project.\n",
			packet.QACycle))
	}

	// For content-mode workflows, inject the most recent synthesis artifact as
	// source material.  This lets synthesis and fix tasks patch an existing
	// document instead of generating a complete new one from scratch, which
	// both improves quality and keeps output tokens manageable.
	if packet.Mode == state.WorkflowModeContent {
		if src := latestSynthesisArtifact(packet.Artifacts); src != nil {
			content := src.Content
			truncated := false
			if len(content) > maxSourceArtifactChars {
				content = content[:maxSourceArtifactChars]
				truncated = true
			}
			sb.WriteString(fmt.Sprintf(
				"\n## Current Document (most recent synthesis — reference or patch this)\nArtifact: %s\nKind: %s\n\n```\n%s\n```\n",
				src.Name, src.Kind, content))
			if truncated {
				sb.WriteString(fmt.Sprintf(
					"[... content truncated at %d chars; the full document continues beyond this point ...]\n",
					maxSourceArtifactChars))
			}
		}
	}

	return sb.String()
}

// latestSynthesisArtifact returns the most recently produced blog_post
// artifact from the list, or the most recent textual synthesis artifact when
// no blog_post exists yet. Returns nil when the list is empty.
func latestSynthesisArtifact(artifacts []state.Artifact) *state.Artifact {
	for i := len(artifacts) - 1; i >= 0; i-- {
		if artifacts[i].Kind == state.ArtifactKindBlogPost {
			return &artifacts[i]
		}
	}
	for i := len(artifacts) - 1; i >= 0; i-- {
		if isTextualSynthesisArtifactKind(artifacts[i].Kind) {
			return &artifacts[i]
		}
	}
	// No synthesized document yet — no source material to inject.
	return nil
}

func isTextualSynthesisArtifactKind(kind state.ArtifactKind) bool {
	if kind == state.ArtifactKindMarkdown || kind == state.ArtifactKindDocument {
		return true
	}
	switch strings.TrimSpace(strings.ToLower(string(kind))) {
	case "plain_text", "plaintext", "text", "txt":
		return true
	default:
		return false
	}
}
