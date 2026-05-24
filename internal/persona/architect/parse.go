package architect

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-orca/go-orca/internal/persona/base"
	"github.com/go-orca/go-orca/internal/state"
)

// flexDesign tolerates common model JSON drifts in design.components.
type flexDesign struct {
	Overview       string                    `json:"overview"`
	Components     state.DesignComponentList `json:"components"`
	Decisions      []state.DesignDecision    `json:"decisions"`
	Diagrams       []string                  `json:"diagrams,omitempty"`
	TechStack      []string                  `json:"tech_stack"`
	DeliveryTarget string                    `json:"delivery_target"`
}

func (d flexDesign) toState() state.Design {
	return state.Design{
		Overview:       d.Overview,
		Components:     []state.DesignComponent(d.Components),
		Decisions:      d.Decisions,
		Diagrams:       d.Diagrams,
		TechStack:      d.TechStack,
		DeliveryTarget: d.DeliveryTarget,
	}
}

type flexArchOutput struct {
	Design  flexDesign `json:"design"`
	Tasks   []taskSpec `json:"tasks"`
	Summary string     `json:"summary"`
}

func parseArchitectOutput(raw string, remediation bool) (archOutput, error) {
	if remediation {
		return parseArchitectRemediation(raw)
	}
	var out archOutput
	if err := base.ParseJSON(raw, &out); err != nil {
		var flex flexArchOutput
		if err2 := base.ParseJSON(raw, &flex); err2 != nil {
			return archOutput{}, err
		}
		out = archOutput{
			Design:  flex.Design.toState(),
			Tasks:   flex.Tasks,
			Summary: flex.Summary,
		}
	}
	return out, nil
}

// parseArchitectRemediation accepts tasks-only or partial design JSON during
// implementation/QA remediation without failing on malformed design.components.
func parseArchitectRemediation(raw string) (archOutput, error) {
	var flex flexArchOutput
	if err := base.ParseJSON(raw, &flex); err != nil {
		var tasksOnly struct {
			Tasks   []taskSpec `json:"tasks"`
			Summary string     `json:"summary"`
		}
		if err2 := base.ParseJSON(raw, &tasksOnly); err2 != nil {
			return archOutput{}, err
		}
		if len(tasksOnly.Tasks) == 0 {
			return archOutput{}, fmt.Errorf("remediation response has no tasks")
		}
		return archOutput{
			Design:  state.Design{Overview: tasksOnly.Summary},
			Tasks:   tasksOnly.Tasks,
			Summary: tasksOnly.Summary,
		}, nil
	}
	if len(flex.Tasks) == 0 {
		return archOutput{}, fmt.Errorf("remediation response has no tasks")
	}
	return archOutput{
		Design:  flex.Design.toState(),
		Tasks:   flex.Tasks,
		Summary: strings.TrimSpace(flex.Summary),
	}, nil
}

// normalizeDesignComponents repairs in-memory design after strict parse.
func normalizeDesignComponents(d *state.Design) {
	if d == nil || len(d.Components) == 0 {
		return
	}
	// Re-marshal through DesignComponentList for any future mixed types.
	b, err := json.Marshal(d.Components)
	if err != nil {
		return
	}
	var list state.DesignComponentList
	if err := json.Unmarshal(b, &list); err != nil {
		return
	}
	d.Components = []state.DesignComponent(list)
}
