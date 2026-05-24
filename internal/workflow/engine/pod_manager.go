package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-orca/go-orca/internal/customization"
	"github.com/go-orca/go-orca/internal/persona"
	"github.com/go-orca/go-orca/internal/state"
)

const podManagerMarkerPrefix = "[pod-manager-pass]"

// runPodManagerCheckin invokes Matriarch as an always-on manager for each pod
// execution pass. It records supervision context in the review thread and
// allows Matriarch to block execution when prerequisites are unmet.
func (e *Engine) runPodManagerCheckin(
	ctx context.Context,
	ws *state.WorkflowState,
	snap *customization.Snapshot,
	pass int,
	taskIndices []int,
) error {
	if !personaRequiredForWorkflow(ws, state.PersonaMatriarch) {
		return nil
	}
	if err := e.checkControlState(ctx, ws); err != nil {
		return err
	}

	p, ok := persona.Get(state.PersonaMatriarch)
	if !ok {
		return fmt.Errorf("matriarch persona not registered")
	}

	taskTitles := make([]string, 0, len(taskIndices))
	taskSubset := make([]state.Task, 0, len(taskIndices))
	for _, idx := range taskIndices {
		if idx < 0 || idx >= len(ws.Tasks) {
			continue
		}
		t := ws.Tasks[idx]
		taskSubset = append(taskSubset, t)
		taskTitles = append(taskTitles, strings.TrimSpace(t.Title))
	}
	if len(taskSubset) == 0 {
		return nil
	}

	marker := fmt.Sprintf("%s %d supervising pod tasks: %s",
		podManagerMarkerPrefix, pass, strings.Join(taskTitles, "; "))
	ws.AllSuggestions = append(ws.AllSuggestions, marker)
	appendReviewThreadEntry(ws, state.ReviewThreadEntry{
		Persona:            state.PersonaPod,
		Kind:               "status",
		Message:            fmt.Sprintf("Pod manager check-in pass %d for tasks: %s", pass, strings.Join(taskTitles, "; ")),
		QACycle:            ws.Execution.QACycle,
		RemediationAttempt: ws.Execution.RemediationAttempt,
	})

	ws.Execution.CurrentPersona = state.PersonaMatriarch
	_ = e.store.SaveWorkflow(ctx, ws)

	packet := e.buildPacket(ws, state.PersonaMatriarch, snap)
	packet.Tasks = taskSubset

	matCtx, matCancel := context.WithTimeout(ctx, e.opts.HandoffTimeout)
	out, err := p.Execute(matCtx, packet)
	matCancel()
	if controlErr := e.checkControlState(ctx, ws); controlErr != nil {
		return controlErr
	}
	if err != nil {
		return fmt.Errorf("pod manager matriarch pass %d: %w", pass, err)
	}

	if ws.Summaries == nil {
		ws.Summaries = make(map[state.PersonaKind]string)
	}
	if summary := strings.TrimSpace(out.Summary); summary != "" {
		prefixed := fmt.Sprintf("[pod-manager pass %d] %s", pass, summary)
		ws.Summaries[state.PersonaMatriarch] += prefixed + "\n"
		e.appendReviewThreadEntries(ws, out, prefixed)
	} else {
		e.appendReviewThreadEntries(ws, out, "")
	}
	if len(out.Suggestions) > 0 {
		ws.AllSuggestions = append(ws.AllSuggestions, out.Suggestions...)
	}
	if err := e.store.SaveWorkflow(ctx, ws); err != nil {
		return err
	}

	if out.MatriarchBlocked {
		reason := out.MatriarchBlockedReason
		if reason == "" {
			reason = "Matriarch blocked pod execution — a hard prerequisite is unmet"
		}
		return fmt.Errorf("matriarch blocked pod execution (pass %d): %s", pass, reason)
	}
	return nil
}

func isPodManagerPass(packet state.HandoffPacket) bool {
	for i := len(packet.AllSuggestions) - 1; i >= 0; i-- {
		msg := strings.TrimSpace(packet.AllSuggestions[i])
		if strings.HasPrefix(msg, podManagerMarkerPrefix) {
			return true
		}
	}
	return false
}
