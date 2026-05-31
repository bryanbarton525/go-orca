package templates

import (
	"testing"

	"github.com/go-orca/go-orca/internal/state"
)

func TestGetSoftwareDefault(t *testing.T) {
	tmpl, ok := Get("software-default")
	if !ok {
		t.Fatal("software-default template missing")
	}
	if tmpl.Mode != state.WorkflowModeSoftware {
		t.Fatalf("mode = %s", tmpl.Mode)
	}
	if len(tmpl.RequiredPersonas) < 4 {
		t.Fatalf("expected personas, got %v", tmpl.RequiredPersonas)
	}
}

func TestApplyTemplate(t *testing.T) {
	ws := state.NewWorkflowState("t", "s", "build api")
	tmpl, _ := Get("software-default")
	Apply(ws, tmpl)
	if ws.Execution.TemplateID != "software-default" {
		t.Fatalf("template id = %q", ws.Execution.TemplateID)
	}
	if len(ws.RequiredPersonas) == 0 {
		t.Fatal("required personas not set")
	}
}
