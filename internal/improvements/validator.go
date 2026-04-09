package improvements

import (
	"fmt"
	"strings"

	"github.com/go-orca/go-orca/internal/state"
)

// ValidateImprovement validates that the files within an improvement conform to
// the expected schema for their component type.
//
//   - skill   → SKILL.md must contain YAML frontmatter with name + description
//   - agent   → .agent.md must contain YAML frontmatter with name, description, model, color
//   - prompt  → any content is accepted (no strict schema enforced)
//   - persona → blocked at the path-policy layer; not validated here
func ValidateImprovement(imp state.RefinerImprovement) error {
	for _, f := range improvementFiles(imp) {
		if err := validateFile(imp.ComponentType, f.Path, f.Content); err != nil {
			return err
		}
	}
	return nil
}

func validateFile(componentType, path, content string) error {
	switch componentType {
	case "skill":
		if strings.HasSuffix(path, "SKILL.md") {
			return validateSkillFrontmatter(content)
		}
	case "agent":
		if strings.HasSuffix(path, ".agent.md") {
			return validateAgentFrontmatter(content)
		}
	}
	return nil
}

// validateSkillFrontmatter verifies that a SKILL.md has YAML frontmatter with
// the required name and description fields.
func validateSkillFrontmatter(content string) error {
	fm := parseFrontmatter(content)
	if fm["name"] == "" {
		return fmt.Errorf("skill SKILL.md: frontmatter missing required field 'name'")
	}
	if fm["description"] == "" {
		return fmt.Errorf("skill SKILL.md: frontmatter missing required field 'description'")
	}
	return nil
}

// validateAgentFrontmatter verifies that an .agent.md has YAML frontmatter with
// the required name, description, model, and color fields.
func validateAgentFrontmatter(content string) error {
	fm := parseFrontmatter(content)
	for _, field := range []string{"name", "description", "model", "color"} {
		if fm[field] == "" {
			return fmt.Errorf("agent .agent.md: frontmatter missing required field %q", field)
		}
	}
	return nil
}

// parseFrontmatter extracts simple key: value pairs from YAML frontmatter
// delimited by "---" fences.  Returns an empty map when no frontmatter is found.
// This is intentionally minimal — it handles the fields needed for schema
// validation without a full YAML parser dependency.
func parseFrontmatter(content string) map[string]string {
	result := make(map[string]string)
	if !strings.HasPrefix(content, "---") {
		return result
	}
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return result
	}
	block := rest[:idx]
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key != "" {
			result[key] = val
		}
	}
	return result
}

// improvementFiles returns the canonical file list for an improvement.
// When imp.Files is non-empty it is returned as-is.  Otherwise a single-file
// list is constructed from imp.Content using the component type/name to derive
// the file path.  Returns nil when there is no content to write.
func improvementFiles(imp state.RefinerImprovement) []state.ImprovementFile {
	if len(imp.Files) > 0 {
		return imp.Files
	}
	if imp.Content == "" {
		return nil
	}
	return []state.ImprovementFile{{
		Path:    legacyRelPath(imp.ComponentType, imp.ComponentName),
		Content: imp.Content,
	}}
}

// legacyRelPath derives the relative file path for a single-file improvement.
func legacyRelPath(componentType, componentName string) string {
	switch componentType {
	case "skill":
		return "skills/" + componentName + "/SKILL.md"
	case "prompt":
		return "prompts/" + componentName + ".prompt.md"
	case "agent":
		return "agents/" + componentName + ".agent.md"
	default:
		return "personas/" + componentName + ".md"
	}
}
