package architect

import (
	"strings"
	"testing"
)

func TestParseArchitectRemediation_MixedComponents(t *testing.T) {
	raw := `{
  "design": {
    "overview": "fix build",
    "components": [
      {"name": "API", "description": "routes"},
      "rate-limiter"
    ],
    "tech_stack": ["Next.js"],
    "delivery_target": "app"
  },
  "tasks": [
    {"title": "Fix package.json", "description": "Repair deps", "assigned_to": "pod", "specialty": "frontend"}
  ],
  "summary": "Remediation cycle 1"
}`
	out, err := parseArchitectRemediation(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Tasks) != 1 {
		t.Fatalf("tasks=%d", len(out.Tasks))
	}
	if len(out.Design.Components) != 2 {
		t.Fatalf("components=%d", len(out.Design.Components))
	}
}

func TestParseArchitectRemediation_TasksOnly(t *testing.T) {
	raw := `{"tasks":[{"title":"Fix build","description":"Run pnpm build","assigned_to":"pod"}],"summary":"cycle 2"}`
	out, err := parseArchitectRemediation(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Tasks) != 1 || !strings.Contains(out.Summary, "cycle") {
		t.Fatalf("unexpected %+v", out)
	}
}
