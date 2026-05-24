package pm

import (
	"testing"

	"github.com/go-orca/go-orca/internal/persona/base"
)

func TestNormalizePMOutputConvertsOutOfScopeString(t *testing.T) {
	t.Parallel()

	raw := `{
		"constitution": {
			"vision": "v",
			"goals": ["g"],
			"constraints": ["c"],
			"audience": "a",
			"acceptance_criteria": ["ok"],
			"out_of_scope": "legacy systems"
		},
		"requirements": {
			"functional": [],
			"non_functional": []
		},
		"summary": "s"
	}`

	fixed, err := normalizePMOutput(raw)
	if err != nil {
		t.Fatalf("normalizePMOutput error: %v", err)
	}

	var out pmOutput
	if err := base.ParseJSON(fixed, &out); err != nil {
		t.Fatalf("normalized parse failed: %v", err)
	}
	if len(out.Constitution.OutOfScope) != 1 || out.Constitution.OutOfScope[0] != "legacy systems" {
		t.Fatalf("out_of_scope = %#v", out.Constitution.OutOfScope)
	}
}

func TestNormalizePMOutputSetsMissingOutOfScopeToEmptyArray(t *testing.T) {
	t.Parallel()

	raw := `{
		"constitution": {
			"vision": "v",
			"goals": ["g"],
			"constraints": ["c"],
			"audience": "a",
			"acceptance_criteria": ["ok"]
		},
		"requirements": {
			"functional": [],
			"non_functional": []
		},
		"summary": "s"
	}`

	fixed, err := normalizePMOutput(raw)
	if err != nil {
		t.Fatalf("normalizePMOutput error: %v", err)
	}

	var out pmOutput
	if err := base.ParseJSON(fixed, &out); err != nil {
		t.Fatalf("normalized parse failed: %v", err)
	}
	if out.Constitution.OutOfScope == nil || len(out.Constitution.OutOfScope) != 0 {
		t.Fatalf("expected empty out_of_scope, got %#v", out.Constitution.OutOfScope)
	}
}
