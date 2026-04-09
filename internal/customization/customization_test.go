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
	// Flat SKILL.md at root → backward-compatible name "skill"
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

// TestSnapshotSkillSubdirectoryName verifies that SKILL.md inside a named
// subdirectory is named after the subdirectory, not the file name.
func TestSnapshotSkillSubdirectoryName(t *testing.T) {
	dir := t.TempDir()
	// Create package-style layout: skills/my-skill/SKILL.md
	skillDir := filepath.Join(dir, "skills", "my-skill")
	must(t, os.MkdirAll(skillDir, 0o755))
	must(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# My Skill"), 0o644))

	reg := customization.NewRegistry()
	reg.AddSource(customization.Source{
		Name:       "test-subdir",
		Type:       "filesystem",
		Root:       dir,
		Precedence: 10,
	})

	snap, err := reg.Snapshot("")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Skills) != 1 {
		t.Fatalf("Skills: got %d, want 1", len(snap.Skills))
	}
	if snap.Skills[0].Name != "my-skill" {
		t.Errorf("Skill name: got %q, want %q", snap.Skills[0].Name, "my-skill")
	}
}

// TestSnapshotFrontmatterStripped verifies that YAML frontmatter is removed
// from Item.Content and exposed via Item.Metadata.
func TestSnapshotFrontmatterStripped(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-tool")
	must(t, os.MkdirAll(skillDir, 0o755))

	content := "---\nname: my-tool\ndescription: A test tool\n---\n# My Tool\n\nBody text."
	must(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644))

	reg := customization.NewRegistry()
	reg.AddSource(customization.Source{
		Name:       "test-fm",
		Type:       "filesystem",
		Root:       dir,
		Precedence: 5,
	})

	snap, err := reg.Snapshot("")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Skills) != 1 {
		t.Fatalf("Skills: got %d, want 1", len(snap.Skills))
	}

	it := snap.Skills[0]

	// Content should not contain the frontmatter block.
	if it.Content == content {
		t.Error("Content still contains raw frontmatter; stripping did not occur")
	}
	if len(it.Content) > 3 && it.Content[:3] == "---" {
		t.Errorf("Content still starts with frontmatter delim: %q", it.Content[:20])
	}

	// Metadata should be populated from frontmatter.
	if it.Metadata == nil {
		t.Fatal("Metadata is nil; expected frontmatter key/value pairs")
	}
	if it.Metadata["name"] != "my-tool" {
		t.Errorf("Metadata[name]: got %q, want %q", it.Metadata["name"], "my-tool")
	}
	if it.Metadata["description"] != "A test tool" {
		t.Errorf("Metadata[description]: got %q, want %q", it.Metadata["description"], "A test tool")
	}

	// Frontmatter name wins over directory name when present.
	if it.Name != "my-tool" {
		t.Errorf("Name: got %q, want %q (frontmatter name should win)", it.Name, "my-tool")
	}
}

// TestSnapshotNoFrontmatter verifies that files without frontmatter have
// empty Metadata and unchanged Content.
func TestSnapshotNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	content := "# Plain Skill\n\nNo frontmatter here."
	must(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644))

	reg := customization.NewRegistry()
	reg.AddSource(customization.Source{Name: "plain", Type: "filesystem", Root: dir, Precedence: 1})

	snap, err := reg.Snapshot("")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Skills) != 1 {
		t.Fatalf("Skills: got %d, want 1", len(snap.Skills))
	}
	it := snap.Skills[0]
	if it.Content != content {
		t.Errorf("Content changed when no frontmatter present: got %q", it.Content)
	}
	if len(it.Metadata) != 0 {
		t.Errorf("Metadata should be empty for no-frontmatter file, got %v", it.Metadata)
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
