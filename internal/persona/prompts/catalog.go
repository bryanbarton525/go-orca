// Package prompts provides the persona prompt catalog — a runtime loader that
// reads all required base persona system prompts from markdown files on disk.
//
// Prompt files live under the root directory (default "prompts/personas").
// Each built-in persona maps to exactly one file. Missing files are a hard
// error: the system fails clearly rather than silently degrading.
//
// The catalog is intentionally separate from the customization registry
// (internal/customization). Persona base prompts are workflow-state (snapshotted
// at workflow start and persisted); customization overlays are additive and
// live in HandoffPacket.PromptsContext.
package prompts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultRoot is the default directory for persona prompt markdown files,
// relative to the process working directory.
const DefaultRoot = "prompts/personas"

// Prompt key constants — one per required markdown file.
// These are the keys used in WorkflowState.PersonaPromptSnapshot and
// HandoffPacket.PersonaPromptSnapshot.
const (
	KeyDirector         = "director"
	KeyProjectManager   = "project_manager"
	KeyEngineerProxy    = "engineer_proxy"
	KeyArchitect        = "architect"
	KeyImplementer      = "implementer"
	KeyQA               = "qa"
	KeyFinalizer        = "finalizer"
	KeyFinalizerRefiner = "finalizer_refiner"
	KeyRefiner          = "refiner"
)

// requiredFiles maps each prompt key to its filename within the root directory.
var requiredFiles = map[string]string{
	KeyDirector:         "director.md",
	KeyProjectManager:   "project_manager.md",
	KeyEngineerProxy:    "engineer_proxy.md",
	KeyArchitect:        "architect.md",
	KeyImplementer:      "implementer.md",
	KeyQA:               "qa.md",
	KeyFinalizer:        "finalizer.md",
	KeyFinalizerRefiner: "finalizer_refiner.md",
	KeyRefiner:          "refiner.md",
}

// Load reads all required persona prompt files from root and returns a map
// of prompt key → file content (trimmed).
//
// All missing or unreadable files are collected and returned as a single
// aggregated error so the caller sees every problem at once rather than
// one at a time. If any file is missing the entire call fails — there is
// no silent fallback.
//
// root may be an absolute path or a path relative to the process working
// directory. Pass prompts.DefaultRoot for the production default.
func Load(root string) (map[string]string, error) {
	out := make(map[string]string, len(requiredFiles))
	var errs []string

	for key, filename := range requiredFiles {
		fullPath := filepath.Join(root, filename)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			errs = append(errs, fmt.Sprintf("  %s: %v", fullPath, err))
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			errs = append(errs, fmt.Sprintf("  %s: file is empty", fullPath))
			continue
		}
		out[key] = content
	}

	if len(errs) > 0 {
		return nil, errors.New("persona prompt catalog: missing or unreadable files:\n" + strings.Join(errs, "\n"))
	}
	return out, nil
}

// Keys returns all required prompt keys in a stable order for iteration.
func Keys() []string {
	return []string{
		KeyDirector,
		KeyProjectManager,
		KeyEngineerProxy,
		KeyArchitect,
		KeyImplementer,
		KeyQA,
		KeyFinalizer,
		KeyFinalizerRefiner,
		KeyRefiner,
	}
}
