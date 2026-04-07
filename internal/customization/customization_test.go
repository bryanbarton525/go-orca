package customization_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-orca/go-orca/internal/customization"
)

// ─── classifyFile (via Snapshot scanning) ─────────────────────────────────────

func TestSnapshotFromFilesystem(t *testing.T) {
	dir := t.TempDir()

	// Create representative files.
	must(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# My Skill"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "coder.agent.md"), []byte("# Coder Agent"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "review.prompt.md"), []byte("# Review Prompt"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("not a skill"), 0o644))

	reg := customization.NewRegistry()
	reg.AddSource(customization.Source{
		Name:       "test-fs",
		Type:       "filesystem",
		Root:       dir,
		Precedence: 10,
	})

	snap, err := reg.Snapshot("")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if len(snap.Skills) != 1 {
		t.Errorf("Skills: got %d, want 1", len(snap.Skills))
	}
	if len(snap.Agents) != 1 {
		t.Errorf("Agents: got %d, want 1", len(snap.Agents))
	}
	if len(snap.Prompts) != 1 {
		t.Errorf("Prompts: got %d, want 1", len(snap.Prompts))
	}
	if len(snap.Skills) > 0 && snap.Skills[0].Name != "skill" {
		t.Errorf("Skill name: got %q, want %q", snap.Skills[0].Name, "skill")
	}
	if len(snap.Agents) > 0 && snap.Agents[0].Name != "coder" {
		t.Errorf("Agent name: got %q, want %q", snap.Agents[0].Name, "coder")
	}
	if len(snap.Prompts) > 0 && snap.Prompts[0].Name != "review" {
		t.Errorf("Prompt name: got %q, want %q", snap.Prompts[0].Name, "review")
	}
}

func TestSnapshotMissingRootSilent(t *testing.T) {
	reg := customization.NewRegistry()
	reg.AddSource(customization.Source{
		Name: "ghost",
		Type: "filesystem",
		Root: "/does/not/exist",
	})
	snap, err := reg.Snapshot("")
	if err != nil {
		t.Fatalf("expected no error for missing root, got: %v", err)
	}
	if len(snap.Skills)+len(snap.Agents)+len(snap.Prompts) != 0 {
		t.Error("expected empty snapshot for missing root")
	}
}

// ─── RegisterBuiltin ──────────────────────────────────────────────────────────

func TestRegisterBuiltin(t *testing.T) {
	reg := customization.NewRegistry()
	reg.RegisterBuiltin(
		&customization.Item{Kind: customization.KindSkill, Name: "built-skill", Content: "builtin skill content"},
		&customization.Item{Kind: customization.KindAgent, Name: "built-agent", Content: "builtin agent content"},
	)
	reg.AddSource(customization.Source{
		Name:       "builtin-src",
		Type:       "builtin",
		Precedence: 100,
	})

	snap, err := reg.Snapshot("")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Skills) != 1 {
		t.Errorf("Skills: got %d, want 1", len(snap.Skills))
	}
	if len(snap.Agents) != 1 {
		t.Errorf("Agents: got %d, want 1", len(snap.Agents))
	}
}

func TestRegisterBuiltinFilteredByEnabledTypes(t *testing.T) {
	reg := customization.NewRegistry()
	reg.RegisterBuiltin(
		&customization.Item{Kind: customization.KindSkill, Name: "s", Content: "skill"},
		&customization.Item{Kind: customization.KindAgent, Name: "a", Content: "agent"},
	)
	// Source only allows skills.
	reg.AddSource(customization.Source{
		Name:         "builtin-skills-only",
		Type:         "builtin",
		Precedence:   100,
		EnabledTypes: []customization.Kind{customization.KindSkill},
	})

	snap, err := reg.Snapshot("")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Skills) != 1 {
		t.Errorf("Skills: got %d, want 1", len(snap.Skills))
	}
	if len(snap.Agents) != 0 {
		t.Errorf("Agents: got %d, want 0 (filtered)", len(snap.Agents))
	}
}

// ─── Deduplication ────────────────────────────────────────────────────────────

func TestSnapshotDeduplication(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// Same skill name in two sources — lower Precedence (0) wins.
	must(t, os.WriteFile(filepath.Join(dir1, "SKILL.md"), []byte("high priority skill"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir2, "SKILL.md"), []byte("low priority skill"), 0o644))

	reg := customization.NewRegistry()
	reg.AddSource(customization.Source{Name: "high", Type: "filesystem", Root: dir1, Precedence: 0})
	reg.AddSource(customization.Source{Name: "low", Type: "filesystem", Root: dir2, Precedence: 50})

	snap, err := reg.Snapshot("")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Skills) != 1 {
		t.Fatalf("Skills: got %d, want 1", len(snap.Skills))
	}
	if snap.Skills[0].Content != "high priority skill" {
		t.Errorf("dedup winner: got %q, want %q", snap.Skills[0].Content, "high priority skill")
	}
}

// ─── ScopeSlug filtering ──────────────────────────────────────────────────────

func TestSnapshotScopeSlugFilter(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("team skill"), 0o644))

	reg := customization.NewRegistry()
	reg.AddSource(customization.Source{
		Name:      "team-src",
		Type:      "filesystem",
		Root:      dir,
		ScopeSlug: "engineering",
	})

	// Different scope slug — should see nothing.
	snap, err := reg.Snapshot("marketing")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Skills) != 0 {
		t.Errorf("expected 0 skills for wrong slug, got %d", len(snap.Skills))
	}

	// Correct slug — should see the skill.
	snap2, err := reg.Snapshot("engineering")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap2.Skills) != 1 {
		t.Errorf("expected 1 skill for matching slug, got %d", len(snap2.Skills))
	}
}

// ─── Context helpers ─────────────────────────────────────────────────────────

func TestSkillsContext(t *testing.T) {
	snap := &customization.Snapshot{
		Skills: []*customization.Item{
			{Name: "alpha", Content: "Alpha content"},
			{Name: "beta", Content: "Beta content"},
		},
	}
	ctx := snap.SkillsContext()
	if ctx == "" {
		t.Error("SkillsContext: expected non-empty string")
	}
}

func TestAgentsContext(t *testing.T) {
	snap := &customization.Snapshot{}
	if snap.AgentsContext() != "" {
		t.Error("empty agents should return empty string")
	}
	snap.Agents = []*customization.Item{{Name: "my-agent", Content: "agent instructions"}}
	if snap.AgentsContext() != "agent instructions" {
		t.Errorf("AgentsContext: got %q", snap.AgentsContext())
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
