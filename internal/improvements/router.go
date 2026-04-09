// Package improvements implements the self-improvement pipeline for go-orca.
//
// The pipeline routes each RefinerImprovement from the inline Finalizer
// retrospective to one of three outcomes:
//
//   - direct  — validated low-risk updates are written straight into
//     artifacts/improvements/active/ and become immediately active.
//   - workflow — higher-risk changes, new components, and persona edits are
//     dispatched as child improvement workflows that open GitHub PRs.
//   - advisory — advisory-only improvements (no file content) emit an event
//     only; nothing is written to disk.
package improvements

import "github.com/go-orca/go-orca/internal/state"

// Apply mode constants mirror state.RefinerImprovement.ApplyMode.
const (
	ApplyModeDirect   = "direct"
	ApplyModeWorkflow = "workflow"
	ApplyModeAdvisory = "advisory"
)

// Route determines the apply mode for a RefinerImprovement using the locked
// routing policy.
//
// Routing rules (evaluated in priority order):
//  1. ChangeType == "advisory" OR no files/content → advisory
//  2. ComponentType == "persona"                   → workflow (always via PR)
//  3. ChangeType == "create"                       → workflow (new components via PR)
//  4. Priority == "medium" || "high"               → workflow
//  5. Priority == "low" + ChangeType == "update" + ComponentType in {skill,prompt} → direct
//  6. default                                      → workflow (safe fallback)
//
// Note: "agent" is intentionally excluded from the direct path — agent files
// are blocked by the surface policy and must always go through a PR workflow.
func Route(imp state.RefinerImprovement) string {
	if imp.ChangeType == "advisory" {
		return ApplyModeAdvisory
	}
	if len(imp.Files) == 0 && imp.Content == "" {
		return ApplyModeAdvisory
	}
	if imp.ComponentType == "persona" {
		return ApplyModeWorkflow
	}
	if imp.ChangeType == "create" {
		return ApplyModeWorkflow
	}
	if imp.Priority == "medium" || imp.Priority == "high" {
		return ApplyModeWorkflow
	}
	if imp.Priority == "low" && imp.ChangeType == "update" {
		switch imp.ComponentType {
		case "skill", "prompt":
			return ApplyModeDirect
		}
	}
	return ApplyModeWorkflow
}
