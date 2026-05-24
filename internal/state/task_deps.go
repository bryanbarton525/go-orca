package state

import (
	"fmt"
	"strings"
)

// ResolveAndSanitizeTaskDependencies rewrites depends_on title references to task IDs,
// removes self-dependencies, and drops edges that cannot be resolved to a task in
// the graph. Returns human-readable warnings for operator visibility.
func ResolveAndSanitizeTaskDependencies(tasks []Task) ([]Task, []string) {
	if len(tasks) == 0 {
		return tasks, nil
	}

	out := make([]Task, len(tasks))
	copy(out, tasks)

	ids := make(map[string]struct{}, len(out))
	titleToID := make(map[string]string, len(out))
	for i := range out {
		ids[out[i].ID] = struct{}{}
		key := normalizeTaskTitle(out[i].Title)
		if key != "" {
			titleToID[key] = out[i].ID
		}
	}

	var warnings []string
	for i := range out {
		resolved := make([]string, 0, len(out[i].DependsOn))
		for _, dep := range out[i].DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}

			targetID := dep
			if _, ok := ids[dep]; !ok {
				if id, ok := titleToID[normalizeTaskTitle(dep)]; ok {
					targetID = id
				} else if id, ok := fuzzyTitleMatch(dep, out); ok {
					targetID = id
					warnings = append(warnings, fmt.Sprintf(
						"[task-graph] fuzzy matched depends_on %q → %q", dep, taskTitle(out, id)))
				} else {
					warnings = append(warnings, fmt.Sprintf(
						"[task-graph] dropped unresolvable depends_on %q from task %q", dep, out[i].Title))
					continue
				}
			}

			if targetID == out[i].ID {
				warnings = append(warnings, fmt.Sprintf(
					"[task-graph] dropped self-dependency on task %q", out[i].Title))
				continue
			}

			resolved = append(resolved, targetID)
		}
		out[i].DependsOn = dedupeStrings(resolved)
	}

	cycleWarnings := breakDependencyCycles(out)
	if len(cycleWarnings) > 0 {
		warnings = append(warnings, cycleWarnings...)
	}

	return out, warnings
}

func normalizeTaskTitle(title string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(title))), " ")
}

func fuzzyTitleMatch(dep string, tasks []Task) (string, bool) {
	needle := normalizeTaskTitle(dep)
	if needle == "" {
		return "", false
	}

	var bestID string
	bestScore := 0
	for _, t := range tasks {
		hay := normalizeTaskTitle(t.Title)
		if hay == "" {
			continue
		}
		score := 0
		switch {
		case hay == needle:
			score = 100
		case strings.Contains(hay, needle):
			score = 80 + len(needle)
		case strings.Contains(needle, hay):
			score = 60 + len(hay)
		}
		if score > bestScore {
			bestScore = score
			bestID = t.ID
		}
	}
	if bestScore < 60 {
		return "", false
	}
	return bestID, true
}

func taskTitle(tasks []Task, id string) string {
	for _, t := range tasks {
		if t.ID == id {
			return t.Title
		}
	}
	return id
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// breakDependencyCycles removes one edge at a time from discovered cycles
// until the dependency graph is acyclic. Edge removals are deterministic.
func breakDependencyCycles(tasks []Task) []string {
	if len(tasks) == 0 {
		return nil
	}
	idToIndex := make(map[string]int, len(tasks))
	for i := range tasks {
		idToIndex[tasks[i].ID] = i
	}

	var warnings []string
	for {
		cycle := firstDependencyCycle(tasks, idToIndex)
		if len(cycle) < 2 {
			break
		}
		fromID := cycle[0]
		toID := cycle[1]
		idx, ok := idToIndex[fromID]
		if !ok {
			break
		}
		before := len(tasks[idx].DependsOn)
		filtered := make([]string, 0, before)
		removed := false
		for _, dep := range tasks[idx].DependsOn {
			if !removed && dep == toID {
				removed = true
				continue
			}
			filtered = append(filtered, dep)
		}
		if !removed {
			break
		}
		tasks[idx].DependsOn = filtered
		warnings = append(warnings, fmt.Sprintf(
			"[task-graph] removed cyclic dependency %q -> %q to break cycle",
			taskTitle(tasks, fromID), taskTitle(tasks, toID),
		))
	}
	return warnings
}

// firstDependencyCycle returns a single cycle as an ordered ID path where
// cycle[0] depends on cycle[1]. Returns nil when acyclic.
func firstDependencyCycle(tasks []Task, idToIndex map[string]int) []string {
	const (
		white = iota
		gray
		black
	)
	color := make(map[string]int, len(tasks))
	stack := make([]string, 0, len(tasks))
	stackPos := make(map[string]int, len(tasks))

	var visit func(id string) []string
	visit = func(id string) []string {
		color[id] = gray
		stackPos[id] = len(stack)
		stack = append(stack, id)

		idx, ok := idToIndex[id]
		if ok {
			for _, dep := range tasks[idx].DependsOn {
				if _, depKnown := idToIndex[dep]; !depKnown {
					continue
				}
				switch color[dep] {
				case white:
					if cycle := visit(dep); len(cycle) > 0 {
						return cycle
					}
				case gray:
					start := stackPos[dep]
					if start >= 0 && start < len(stack) {
						cycle := append([]string(nil), stack[start:]...)
						cycle = append(cycle, dep)
						if len(cycle) >= 2 {
							return cycle
						}
					}
				}
			}
		}

		stack = stack[:len(stack)-1]
		delete(stackPos, id)
		color[id] = black
		return nil
	}

	for _, t := range tasks {
		if color[t.ID] != white {
			continue
		}
		if cycle := visit(t.ID); len(cycle) > 1 {
			return cycle
		}
	}
	return nil
}
