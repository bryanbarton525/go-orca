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
	KeyMatriarch        = "matriarch"
	KeyArchitect        = "architect"
	KeyPod              = "pod"
	KeyQA               = "qa"
	KeyFinalizer        = "finalizer"
	KeyFinalizerRefiner = "finalizer_refiner"
	KeyRefiner          = "refiner"

	// Pod specialty overlays.  Loaded in addition to the base KeyPod prompt
	// and concatenated by the pod persona at runtime when a task carries a
	// matching Specialty.  These files are OPTIONAL — a missing overlay is
	// not a fatal error; the engine simply runs the generic pod prompt.
	KeyPodBackend  = "pod_backend"
	KeyPodFrontend = "pod_frontend"
	KeyPodWriter   = "pod_writer"
	KeyPodOps      = "pod_ops"
	KeyPodData     = "pod_data"
)

// requiredFiles maps each prompt key to its filename within the root directory.
var requiredFiles = map[string]string{
	KeyDirector:         "director.md",
	KeyProjectManager:   "project_manager.md",
	KeyMatriarch:        "matriarch.md",
	KeyArchitect:        "architect.md",
	KeyPod:              "pod.md",
	KeyQA:               "qa.md",
	KeyFinalizer:        "finalizer.md",
	KeyFinalizerRefiner: "finalizer_refiner.md",
	KeyRefiner:          "refiner.md",
}

// optionalFiles maps optional prompt keys to filenames.  Missing files are
// skipped silently — they're enhancements, not requirements.
var optionalFiles = map[string]string{
	KeyPodBackend:  "pod_backend.md",
	KeyPodFrontend: "pod_frontend.md",
	KeyPodWriter:   "pod_writer.md",
	KeyPodOps:      "pod_ops.md",
	KeyPodData:     "pod_data.md",
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

	// Optional files — silently skip when missing or empty.  Operators can
	// drop in a single overlay (e.g. pod_backend.md) without having to
	// provide every specialty.
	for key, filename := range optionalFiles {
		fullPath := filepath.Join(root, filename)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		out[key] = content
	}

	if len(errs) > 0 {
		return nil, errors.New("persona prompt catalog: missing or unreadable files:\n" + strings.Join(errs, "\n"))
	}
	return out, nil
}

// KeyForPodSpecialty resolves a free-text specialty string to a catalog key.
// Returns "" when the specialty is empty or unrecognised; callers should fall
// back to KeyPod in that case.  Recognised aliases map orca-themed names
// (bull, scout, scribe, engineer, tracker) to the canonical keys so the
// Architect can pick whichever vocabulary fits the workflow.
func KeyForPodSpecialty(specialty string) string {
	switch strings.ToLower(strings.TrimSpace(specialty)) {
	case "backend", "back-end", "back_end", "server", "api",
		"bull": // orca alpha bull — heavy lifter, structural code
		return KeyPodBackend
	case "frontend", "front-end", "front_end", "ui", "web",
		"scout": // orca scout — fast, exploratory, visual
		return KeyPodFrontend
	case "writer", "writing", "docs", "documentation", "content", "blog",
		"scribe": // orca scribe — vocalisations, prose
		return KeyPodWriter
	case "ops", "devops", "infra", "infrastructure", "platform",
		"engineer": // orca engineer — builders/structural
		return KeyPodOps
	case "data", "etl", "analytics", "ml",
		"tracker": // orca tracker — patterns, signals
		return KeyPodData
	default:
		return ""
	}
}

// Keys returns all required prompt keys in a stable order for iteration.
func Keys() []string {
	return []string{
		KeyDirector,
		KeyProjectManager,
		KeyMatriarch,
		KeyArchitect,
		KeyPod,
		KeyQA,
		KeyFinalizer,
		KeyFinalizerRefiner,
		KeyRefiner,
	}
}
