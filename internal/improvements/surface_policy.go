package improvements

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-orca/go-orca/internal/state"
)

// ─── Improvement Surface Policy ───────────────────────────────────────────────
//
// The improvement pipeline may ONLY target markdown prompt/persona assets and
// skill packages.  It must never evaluate, modify, or open PRs against the
// workflow engine, API handlers, storage layer, provider integrations, or any
// other Go source.
//
// Allowed component types: "persona", "prompt", "skill"
//
// Allowed file path prefixes (relative):
//   - prompts/personas/    ← base persona prompt markdown files
//   - skills/<name>/      ← skill package: SKILL.md + references/ + scripts/
//
// Explicitly blocked:
//   - agents/             ← .agent.md files are not in scope for auto-refinement
//   - internal/           ← engine, API, storage, providers, scheduler
//   - cmd/                ← binaries
//   - pkg/                ← library code (if present)
//   - docs/               ← generated docs
//   - Any absolute path or path containing ".." traversal

// AllowedComponentTypes is the set of component_type values that the refiner
// may produce. Anything outside this set is silently dropped.
var AllowedComponentTypes = map[string]bool{
	"persona": true,
	"prompt":  true,
	"skill":   true,
}

// allowedPathPrefixes are the relative path prefixes that an improvement file
// may live under.  Order matters: the first match wins for descriptive errors.
var allowedPathPrefixes = []string{
	"prompts/personas/",
	"skills/",
}

// ValidateSurface returns an error when the improvement's component type or any
// of its file paths fall outside the allowed improvement surface.
//
// It is called for every improvement before dispatch — both the direct-apply
// path and the child-workflow path — so the restriction cannot be bypassed.
func ValidateSurface(imp state.RefinerImprovement) error {
	if err := validateComponentType(imp.ComponentType); err != nil {
		return err
	}
	for _, f := range imp.Files {
		if err := validateSurfacePath(f.Path); err != nil {
			return fmt.Errorf("%w (file: %q)", err, f.Path)
		}
	}
	// Legacy single-file path derived from Content.
	if len(imp.Files) == 0 && imp.Content != "" {
		p := legacyRelPath(imp.ComponentType, imp.ComponentName)
		if err := validateSurfacePath(p); err != nil {
			return fmt.Errorf("%w (derived path: %q)", err, p)
		}
	}
	return nil
}

// IsSurfaceAllowed returns true when the improvement is entirely within the
// allowed surface. It is the predicate form of ValidateSurface — used by
// normalizeImprovements to silently drop out-of-scope improvements.
func IsSurfaceAllowed(imp state.RefinerImprovement) bool {
	return ValidateSurface(imp) == nil
}

func validateComponentType(ct string) error {
	if !AllowedComponentTypes[ct] {
		return fmt.Errorf(
			"surface policy: component_type %q is not in the allowed improvement surface "+
				"(allowed: persona, prompt, skill); workflow-engine and codebase changes must "+
				"be made by a human developer",
			ct,
		)
	}
	return nil
}

func validateSurfacePath(relPath string) error {
	if filepath.IsAbs(relPath) {
		return fmt.Errorf("surface policy: absolute path not allowed: %q", relPath)
	}
	cleaned := filepath.ToSlash(filepath.Clean(relPath))
	if strings.Contains(cleaned, "..") {
		return fmt.Errorf("surface policy: path traversal not allowed: %q", relPath)
	}
	for _, prefix := range allowedPathPrefixes {
		if strings.HasPrefix(cleaned+"/", prefix) || strings.HasPrefix(cleaned, prefix) {
			return nil
		}
	}
	return fmt.Errorf(
		"surface policy: path %q is outside the allowed improvement surface "+
			"(allowed prefixes: %s); improvements may only target persona markdown and skill packages",
		relPath,
		strings.Join(allowedPathPrefixes, ", "),
	)
}
