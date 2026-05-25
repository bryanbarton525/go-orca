package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/go-orca/go-orca/internal/mcp/toolchaindeps"
	"github.com/go-orca/go-orca/internal/state"
)

func (e *Engine) toolchainGuidanceContext(toolchainID string) string {
	if e.opts.MCPRegistry != nil {
		return e.opts.MCPRegistry.GuidanceForToolchain(toolchainID)
	}
	return ""
}

// filterDuplicateRemediationTasks drops remediation tasks whose normalized title
// already exists on the workflow from a prior implementation_validation cycle.
func filterDuplicateRemediationTasks(ws *state.WorkflowState, tasks []state.Task, source string, cycle int) []state.Task {
	if len(tasks) == 0 {
		return tasks
	}
	seen := remediationTitleIndex(ws, source, cycle)
	out := make([]state.Task, 0, len(tasks))
	for _, t := range tasks {
		key := normalizeRemediationTitle(t.Title)
		if key == "" {
			out = append(out, t)
			continue
		}
		if seen[key] {
			if ws != nil {
				ws.AllSuggestions = append(ws.AllSuggestions,
					fmt.Sprintf("[remediation] skipped duplicate task title %q (cycle %d)", t.Title, cycle))
			}
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return out
}

func remediationTitleIndex(ws *state.WorkflowState, source string, currentCycle int) map[string]bool {
	seen := make(map[string]bool)
	if ws == nil {
		return seen
	}
	for _, t := range ws.Tasks {
		if t.RemediationSource != source {
			continue
		}
		if t.Attempt > 0 && t.Attempt >= currentCycle {
			continue
		}
		key := normalizeRemediationTitle(t.Title)
		if key != "" {
			seen[key] = true
		}
	}
	return seen
}

func normalizeRemediationTitle(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	t = strings.ReplaceAll(t, "—", "-")
	for _, prefix := range []string{"re-run ", "rerun ", "re-run: ", "install ", "run "} {
		if strings.HasPrefix(t, prefix) {
			// keep full title for dedupe — identical titles only
		}
	}
	return t
}

// packageJSONRemediationTask returns a deterministic pod task when package.json is invalid.
func packageJSONRemediationTask(ws *state.WorkflowState, source string, cycle int) *state.Task {
	if ws == nil || ws.Execution.Workspace == nil {
		return nil
	}
	if !blockersMentionInvalidPackageJSON(ws.BlockingIssues) {
		return nil
	}
	ok, _ := toolchaindeps.CheckPackageJSON(ws.Execution.Workspace.Path)
	if ok {
		return nil
	}
	title := "Fix package.json strict JSON syntax"
	if remediationTitleIndex(ws, source, cycle)[normalizeRemediationTitle(title)] {
		return nil
	}
	return &state.Task{
		ID:                uuid.New().String(),
		WorkflowID:        ws.ID,
		Title:             title,
		Description:       "Rewrite " + ws.Execution.Workspace.Path + "/package.json as strict JSON. Remove every leading comment line (lines starting with // or /*). Do not add prose such as \"Contents of updated package.json\". The file must parse with json.Unmarshal and start with {. After fixing the file, do not run pnpm install from the shell — the engine will validate via install_dependencies.",
		Status:            state.TaskStatusPending,
		AssignedTo:        state.PersonaPod,
		Specialty:         "frontend",
		RemediationSource: source,
		Attempt:           cycle,
		CreatedAt:         time.Now().UTC(),
	}
}

func blockersMentionInvalidPackageJSON(blockers []string) bool {
	for _, b := range blockers {
		lower := strings.ToLower(b)
		if strings.Contains(lower, "package.json") ||
			strings.Contains(lower, "not valid json") ||
			strings.Contains(lower, "unexpected token") {
			return true
		}
	}
	return false
}
