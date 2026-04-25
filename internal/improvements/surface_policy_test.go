package improvements_test

import (
	"testing"

	"github.com/go-orca/go-orca/internal/improvements"
	"github.com/go-orca/go-orca/internal/state"
)

// ─── ValidateSurface ──────────────────────────────────────────────────────────

func validPersonaImp() state.RefinerImprovement {
	return state.RefinerImprovement{
		ComponentType: "persona",
		ComponentName: "pod",
		Problem:       "p",
		ProposedFix:   "f",
		Priority:      "high",
		ChangeType:    "update",
		Files: []state.ImprovementFile{
			{Path: "prompts/personas/pod.md", Content: "# impl"},
		},
	}
}

func validSkillImp() state.RefinerImprovement {
	return state.RefinerImprovement{
		ComponentType: "skill",
		ComponentName: "my-skill",
		Problem:       "p",
		ProposedFix:   "f",
		Priority:      "low",
		ChangeType:    "update",
		Files: []state.ImprovementFile{
			{Path: "skills/my-skill/SKILL.md", Content: "---\nname: my-skill\ndescription: d\n---\n"},
		},
	}
}

func TestValidateSurface_AllowsPersonaFile(t *testing.T) {
	if err := improvements.ValidateSurface(validPersonaImp()); err != nil {
		t.Errorf("expected no error for persona file, got: %v", err)
	}
}

func TestValidateSurface_AllowsSkillSKILLmd(t *testing.T) {
	if err := improvements.ValidateSurface(validSkillImp()); err != nil {
		t.Errorf("expected no error for skill SKILL.md, got: %v", err)
	}
}

func TestValidateSurface_AllowsSkillReferencesFile(t *testing.T) {
	imp := validSkillImp()
	imp.Files = []state.ImprovementFile{
		{Path: "skills/my-skill/references/guide.md", Content: "# guide"},
	}
	if err := improvements.ValidateSurface(imp); err != nil {
		t.Errorf("expected no error for skill references file, got: %v", err)
	}
}

func TestValidateSurface_AllowsSkillScriptsFile(t *testing.T) {
	imp := validSkillImp()
	imp.Files = []state.ImprovementFile{
		{Path: "skills/my-skill/scripts/check.sh", Content: "#!/bin/sh\necho ok"},
	}
	if err := improvements.ValidateSurface(imp); err != nil {
		t.Errorf("expected no error for skill scripts file, got: %v", err)
	}
}

func TestValidateSurface_AllowsPromptType(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "prompt",
		ComponentName: "delivery",
		Problem:       "p",
		ProposedFix:   "f",
		Priority:      "low",
		ChangeType:    "update",
		Files: []state.ImprovementFile{
			{Path: "prompts/personas/delivery.md", Content: "# delivery"},
		},
	}
	if err := improvements.ValidateSurface(imp); err != nil {
		t.Errorf("expected no error for prompt type, got: %v", err)
	}
}

func TestValidateSurface_RejectsAgentComponentType(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "agent",
		ComponentName: "my-agent",
		Problem:       "p",
		ProposedFix:   "f",
		Priority:      "low",
		ChangeType:    "update",
		Files: []state.ImprovementFile{
			{Path: "agents/my-agent.agent.md", Content: "# agent"},
		},
	}
	if err := improvements.ValidateSurface(imp); err == nil {
		t.Error("expected error for agent component type, got nil")
	}
}

func TestValidateSurface_RejectsWorkflowComponentType(t *testing.T) {
	imp := state.RefinerImprovement{
		ComponentType: "workflow",
		ComponentName: "engine",
		Problem:       "p",
		ProposedFix:   "f",
		Priority:      "low",
		ChangeType:    "advisory",
	}
	if err := improvements.ValidateSurface(imp); err == nil {
		t.Error("expected error for workflow component type, got nil")
	}
}

func TestValidateSurface_RejectsGoSourcePath(t *testing.T) {
	imp := validPersonaImp()
	imp.Files = []state.ImprovementFile{
		{Path: "internal/workflow/engine/engine.go", Content: "package engine"},
	}
	if err := improvements.ValidateSurface(imp); err == nil {
		t.Error("expected error for Go source path, got nil")
	}
}

func TestValidateSurface_RejectsAgentFilePath(t *testing.T) {
	imp := validPersonaImp()
	imp.Files = []state.ImprovementFile{
		{Path: "agents/my-agent.agent.md", Content: "# agent"},
	}
	if err := improvements.ValidateSurface(imp); err == nil {
		t.Error("expected error for agents/ path, got nil")
	}
}

func TestValidateSurface_RejectsCmdPath(t *testing.T) {
	imp := validPersonaImp()
	imp.Files = []state.ImprovementFile{
		{Path: "cmd/go-orca-api/main.go", Content: "package main"},
	}
	if err := improvements.ValidateSurface(imp); err == nil {
		t.Error("expected error for cmd/ path, got nil")
	}
}

func TestValidateSurface_RejectsAbsolutePath(t *testing.T) {
	imp := validPersonaImp()
	imp.Files = []state.ImprovementFile{
		{Path: "/etc/passwd", Content: "root"},
	}
	if err := improvements.ValidateSurface(imp); err == nil {
		t.Error("expected error for absolute path, got nil")
	}
}

func TestValidateSurface_RejectsPathTraversal(t *testing.T) {
	imp := validPersonaImp()
	imp.Files = []state.ImprovementFile{
		{Path: "skills/../../internal/engine.go", Content: "evil"},
	}
	if err := improvements.ValidateSurface(imp); err == nil {
		t.Error("expected error for traversal path, got nil")
	}
}

func TestValidateSurface_LegacyContentPersona(t *testing.T) {
	// Legacy single-file improvement using Content field, no Files slice.
	imp := state.RefinerImprovement{
		ComponentType: "persona",
		ComponentName: "pod",
		Problem:       "p",
		ProposedFix:   "f",
		Priority:      "high",
		ChangeType:    "update",
		Content:       "# impl",
	}
	// legacyRelPath derives "personas/pod.md" — that does NOT start
	// with "prompts/personas/", so it should be rejected.
	if err := improvements.ValidateSurface(imp); err == nil {
		t.Error("expected error for legacy persona path (personas/ not prompts/personas/), got nil")
	}
}

func TestValidateSurface_LegacyContentSkill(t *testing.T) {
	// Legacy single-file improvement using Content field.
	imp := state.RefinerImprovement{
		ComponentType: "skill",
		ComponentName: "my-skill",
		Problem:       "p",
		ProposedFix:   "f",
		Priority:      "low",
		ChangeType:    "update",
		Content:       "---\nname: my-skill\ndescription: d\n---\n",
	}
	// legacyRelPath derives "skills/my-skill/SKILL.md" — allowed.
	if err := improvements.ValidateSurface(imp); err != nil {
		t.Errorf("expected no error for legacy skill content, got: %v", err)
	}
}

// ─── IsSurfaceAllowed ─────────────────────────────────────────────────────────

func TestIsSurfaceAllowed_TrueForAllowedPersona(t *testing.T) {
	if !improvements.IsSurfaceAllowed(validPersonaImp()) {
		t.Error("expected IsSurfaceAllowed=true for valid persona improvement")
	}
}

func TestIsSurfaceAllowed_FalseForAgentType(t *testing.T) {
	imp := validPersonaImp()
	imp.ComponentType = "agent"
	if improvements.IsSurfaceAllowed(imp) {
		t.Error("expected IsSurfaceAllowed=false for agent component type")
	}
}

func TestIsSurfaceAllowed_FalseForGoSourcePath(t *testing.T) {
	imp := validPersonaImp()
	imp.Files = []state.ImprovementFile{
		{Path: "internal/improvements/dispatcher.go", Content: "evil"},
	}
	if improvements.IsSurfaceAllowed(imp) {
		t.Error("expected IsSurfaceAllowed=false for Go source path")
	}
}
