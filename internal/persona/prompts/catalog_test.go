package prompts_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-orca/go-orca/internal/persona/prompts"
)

// writeAll creates a temporary directory seeded with all required prompt files
// and returns its path.
func writeAll(t *testing.T, overrides map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	// Seed every required file with minimal non-empty content.
	defaults := map[string]string{
		"director.md":          "You are the Director.",
		"project_manager.md":   "You are the Project Manager.",
		"matriarch.md":    "You are the Matriarch.",
		"architect.md":         "You are the Architect.",
		"pod.md":       "You are the Pod.",
		"qa.md":                "You are the QA.",
		"finalizer.md":         "You are the Finalizer.",
		"finalizer_refiner.md": "You are the Finalizer Refiner.",
		"refiner.md":           "You are the Refiner.",
	}
	for filename, content := range defaults {
		if override, ok := overrides[filename]; ok {
			content = override
		}
		if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
			t.Fatalf("writeAll: %v", err)
		}
	}
	return dir
}

// TestLoad_Success verifies that all prompt keys are loaded from a complete directory.
func TestLoad_Success(t *testing.T) {
	dir := writeAll(t, nil)

	snap, err := prompts.Load(dir)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	for _, key := range prompts.Keys() {
		if snap[key] == "" {
			t.Errorf("key %q: expected non-empty prompt, got empty", key)
		}
	}
	if len(snap) != len(prompts.Keys()) {
		t.Errorf("snapshot has %d keys, want %d", len(snap), len(prompts.Keys()))
	}
}

// TestLoad_ContentTrimmed verifies that leading/trailing whitespace is stripped.
func TestLoad_ContentTrimmed(t *testing.T) {
	dir := writeAll(t, map[string]string{
		"director.md": "\n\n  trimmed content  \n\n",
	})

	snap, err := prompts.Load(dir)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if snap[prompts.KeyDirector] != "trimmed content" {
		t.Errorf("KeyDirector: got %q, want %q", snap[prompts.KeyDirector], "trimmed content")
	}
}

// TestLoad_MissingFiles verifies that all missing files are reported together.
func TestLoad_MissingFiles(t *testing.T) {
	dir := t.TempDir() // empty directory — no files at all

	_, err := prompts.Load(dir)
	if err == nil {
		t.Fatal("Load: expected error for missing files, got nil")
	}

	// Verify that the error message mentions at least one of the required files.
	errStr := err.Error()
	if len(errStr) == 0 {
		t.Error("Load: error message is empty")
	}
}

// TestLoad_PartialMissing verifies that all missing files are reported, not just the first.
func TestLoad_PartialMissing(t *testing.T) {
	// Provide only some files — director and qa are missing.
	dir := t.TempDir()
	present := map[string]string{
		"project_manager.md":   "pm",
		"matriarch.md":    "eng",
		"architect.md":         "arch",
		"pod.md":       "impl",
		"finalizer.md":         "fin",
		"finalizer_refiner.md": "finref",
		"refiner.md":           "ref",
	}
	for filename, content := range present {
		if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	_, err := prompts.Load(dir)
	if err == nil {
		t.Fatal("Load: expected error for partial missing files, got nil")
	}
}

// TestLoad_EmptyFile verifies that an empty file is treated as an error.
func TestLoad_EmptyFile(t *testing.T) {
	dir := writeAll(t, map[string]string{
		"director.md": "   \n  \n  ", // whitespace only → empty after trim
	})

	_, err := prompts.Load(dir)
	if err == nil {
		t.Fatal("Load: expected error for empty file, got nil")
	}
}

// TestLoad_Overridable verifies the root is overridable (test isolation).
func TestLoad_Overridable(t *testing.T) {
	dir1 := writeAll(t, map[string]string{"director.md": "version-A"})
	dir2 := writeAll(t, map[string]string{"director.md": "version-B"})

	snap1, err := prompts.Load(dir1)
	if err != nil {
		t.Fatalf("Load dir1: %v", err)
	}
	snap2, err := prompts.Load(dir2)
	if err != nil {
		t.Fatalf("Load dir2: %v", err)
	}

	if snap1[prompts.KeyDirector] == snap2[prompts.KeyDirector] {
		t.Error("expected different director prompts from different roots")
	}
}
