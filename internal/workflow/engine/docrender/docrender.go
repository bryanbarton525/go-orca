// Package docrender renders typed workflow state into deterministic markdown.
//
// The engine writes the rendered output to the workflow's workspace (or
// persists it as an Artifact for non-workspace flows) so downstream personas
// consume a single source of truth instead of re-receiving JSON-encoded
// goalposts in every prompt. See plan in /Users/bbarton/.claude/plans/lazy-orbiting-forest.md.
package docrender

import (
	"fmt"
	"sort"
	"strings"

	"github.com/go-orca/go-orca/internal/state"
)

// ConstitutionFile is the canonical filename for the PM-authored charter doc.
const ConstitutionFile = "constitution.md"

// PlanFile is the canonical filename for the Architect-authored plan doc.
const PlanFile = "plan.md"

// RenderConstitution renders the PM constitution + requirements as markdown.
// Either pointer may be nil; an empty document is returned when both are nil.
func RenderConstitution(c *state.Constitution, r *state.Requirements) string {
	var sb strings.Builder
	sb.WriteString("# Constitution\n\n")
	sb.WriteString("> Authored by the Project Manager. Immutable for the rest of the workflow; remediation triage may append a `Constitution Amendment` section but cannot rewrite this file.\n\n")

	if c != nil {
		writeField(&sb, "Vision", c.Vision)
		writeList(&sb, "Goals", c.Goals)
		writeList(&sb, "Constraints", c.Constraints)
		writeField(&sb, "Audience", c.Audience)
		writeField(&sb, "Output Medium", c.OutputMedium)
		writeList(&sb, "Acceptance Criteria", c.AcceptanceCriteria)
		writeList(&sb, "Out of Scope", c.OutOfScope)
	}

	if r != nil {
		if len(r.Functional) > 0 {
			sb.WriteString("## Functional Requirements\n\n")
			writeRequirementTable(&sb, r.Functional)
		}
		if len(r.NonFunctional) > 0 {
			sb.WriteString("## Non-Functional Requirements\n\n")
			writeRequirementTable(&sb, r.NonFunctional)
		}
		writeList(&sb, "Dependencies", r.Dependencies)
	}

	return sb.String()
}

// RenderPlan renders the Architect design + initial task graph as markdown.
// Either input may be nil/empty.
func RenderPlan(d *state.Design, tasks []state.Task) string {
	var sb strings.Builder
	sb.WriteString("# Plan\n\n")
	sb.WriteString("> Authored by the Architect. The initial section below is the primary plan; remediation cycles append `## Remediation Cycle N` sections and never rewrite this header.\n\n")

	if d != nil {
		writeField(&sb, "Overview", d.Overview)
		writeField(&sb, "Delivery Target", d.DeliveryTarget)
		writeList(&sb, "Tech Stack", d.TechStack)

		if len(d.Components) > 0 {
			sb.WriteString("## Components\n\n")
			sb.WriteString("| Name | Description | Inputs | Outputs |\n")
			sb.WriteString("|---|---|---|---|\n")
			for _, c := range d.Components {
				sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
					escapeCell(c.Name), escapeCell(c.Description),
					escapeCell(strings.Join(c.Inputs, ", ")),
					escapeCell(strings.Join(c.Outputs, ", "))))
			}
			sb.WriteString("\n")
		}

		if len(d.Decisions) > 0 {
			sb.WriteString("## Architectural Decisions\n\n")
			for i, dec := range d.Decisions {
				sb.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, dec.Decision))
				if dec.Rationale != "" {
					sb.WriteString(fmt.Sprintf("   - Rationale: %s\n", dec.Rationale))
				}
				if dec.Tradeoffs != "" {
					sb.WriteString(fmt.Sprintf("   - Tradeoffs: %s\n", dec.Tradeoffs))
				}
			}
			sb.WriteString("\n")
		}
	}

	// Initial pass shows tasks with Attempt == 0 (or unset). Later remediation
	// tasks appear in their own appended sections via RenderRemediationSection.
	initial := filterTasks(tasks, func(t state.Task) bool { return t.Attempt == 0 })
	if len(initial) > 0 {
		sb.WriteString("## Task Graph\n\n")
		writeTaskTable(&sb, initial)
	}

	return sb.String()
}

// RenderTriageSection renders a PM remediation-triage entry suitable for
// appending to plan.md. brief is the PM's free-text classification summary;
// blockingIssues lists the QA-reported issues the PM was triaging.
func RenderTriageSection(cycle int, brief string, blockingIssues []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n---\n\n## Remediation Cycle %d — PM Triage\n\n", cycle))
	if strings.TrimSpace(brief) != "" {
		sb.WriteString(brief)
		sb.WriteString("\n\n")
	}
	if len(blockingIssues) > 0 {
		sb.WriteString("**QA blocking issues being triaged:**\n\n")
		for _, issue := range blockingIssues {
			sb.WriteString(fmt.Sprintf("- %s\n", issue))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// RenderRemediationSection renders an Architect remediation-planning entry.
// It lists only tasks tagged with the given cycle (Attempt == cycle) and any
// design notes the Architect added for that cycle.
func RenderRemediationSection(cycle int, d *state.Design, tasks []state.Task) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n---\n\n## Remediation Cycle %d — Architect\n\n", cycle))

	cycleTasks := filterTasks(tasks, func(t state.Task) bool { return t.Attempt == cycle })
	if len(cycleTasks) == 0 {
		sb.WriteString("_No remediation tasks were emitted for this cycle._\n")
		return sb.String()
	}

	if d != nil && d.Overview != "" {
		// Only append the design-overview line when the Architect updated it
		// during the remediation pass. We can't detect "changed since last
		// cycle" here, so we always include the current Overview as a
		// short reference; the file already has the original Overview at the
		// top, so this is intentional duplication when the Architect rewrote
		// the same text.
		sb.WriteString(fmt.Sprintf("**Current overview:** %s\n\n", d.Overview))
	}

	sb.WriteString("### Remediation Tasks\n\n")
	writeTaskTable(&sb, cycleTasks)
	return sb.String()
}

// RenderConstitutionAmendment renders an inline amendment to be appended to
// constitution.md when PM remediation triage classifies a real requirement gap.
// The original constitution remains intact above this section.
func RenderConstitutionAmendment(cycle int, brief string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n---\n\n## Constitution Amendment — Cycle %d\n\n", cycle))
	if strings.TrimSpace(brief) != "" {
		sb.WriteString(brief)
		sb.WriteString("\n")
	}
	return sb.String()
}

// ── helpers ──────────────────────────────────────────────────────────────────

func writeField(sb *strings.Builder, heading, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	sb.WriteString(fmt.Sprintf("## %s\n\n%s\n\n", heading, value))
}

func writeList(sb *strings.Builder, heading string, items []string) {
	nonEmpty := nonEmptyStrings(items)
	if len(nonEmpty) == 0 {
		return
	}
	sb.WriteString(fmt.Sprintf("## %s\n\n", heading))
	for _, item := range nonEmpty {
		sb.WriteString(fmt.Sprintf("- %s\n", item))
	}
	sb.WriteString("\n")
}

func writeRequirementTable(sb *strings.Builder, reqs []state.Requirement) {
	sb.WriteString("| ID | Priority | Title | Description | Source |\n")
	sb.WriteString("|---|---|---|---|---|\n")
	for _, r := range reqs {
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
			escapeCell(r.ID), escapeCell(r.Priority),
			escapeCell(r.Title), escapeCell(r.Description),
			escapeCell(r.Source)))
	}
	sb.WriteString("\n")
}

func writeTaskTable(sb *strings.Builder, tasks []state.Task) {
	// Group by specialty for readability; "" buckets last.
	grouped := make(map[string][]state.Task)
	specialties := make([]string, 0)
	for _, t := range tasks {
		s := t.Specialty
		if _, exists := grouped[s]; !exists {
			specialties = append(specialties, s)
		}
		grouped[s] = append(grouped[s], t)
	}
	sort.SliceStable(specialties, func(i, j int) bool {
		// "" goes last so untyped tasks don't lead the table.
		if specialties[i] == "" {
			return false
		}
		if specialties[j] == "" {
			return true
		}
		return specialties[i] < specialties[j]
	})

	sb.WriteString("| ID | Specialty | Title | Depends On | Description |\n")
	sb.WriteString("|---|---|---|---|---|\n")
	for _, s := range specialties {
		for _, t := range grouped[s] {
			id := t.ID
			if len(id) > 8 {
				id = id[:8]
			}
			specialty := t.Specialty
			if specialty == "" {
				specialty = "-"
			}
			deps := "-"
			if len(t.DependsOn) > 0 {
				short := make([]string, 0, len(t.DependsOn))
				for _, d := range t.DependsOn {
					if len(d) > 8 {
						short = append(short, d[:8])
					} else {
						short = append(short, d)
					}
				}
				deps = strings.Join(short, ", ")
			}
			sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
				escapeCell(id), escapeCell(specialty),
				escapeCell(t.Title), escapeCell(deps),
				escapeCell(t.Description)))
		}
	}
	sb.WriteString("\n")
}

// escapeCell makes text safe inside a single markdown table cell: no raw
// pipes, no embedded newlines.
func escapeCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func nonEmptyStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func filterTasks(tasks []state.Task, pred func(state.Task) bool) []state.Task {
	out := make([]state.Task, 0, len(tasks))
	for _, t := range tasks {
		if pred(t) {
			out = append(out, t)
		}
	}
	return out
}
