package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/go-orca/go-orca/internal/customization"
	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/logger"
	"github.com/go-orca/go-orca/internal/persona"
	"github.com/go-orca/go-orca/internal/state"
)

func (e *Engine) podConcurrency() int {
	if e.opts.MaxConcurrentTasks > 1 {
		return e.opts.MaxConcurrentTasks
	}
	return 1
}

func (e *Engine) readyPodTaskIndices(ws *state.WorkflowState) []int {
	var indices []int
	for i := range ws.Tasks {
		t := &ws.Tasks[i]
		if state.PersonaKind(strings.ToLower(strings.TrimSpace(string(t.AssignedTo)))) != state.PersonaPod {
			continue
		}
		if !e.taskDependenciesSatisfied(ws, t) {
			if t.Status == state.TaskStatusReady {
				t.Status = state.TaskStatusPending
			}
			continue
		}
		switch t.Status {
		case state.TaskStatusReady, state.TaskStatusPending, state.TaskStatusFailed:
			indices = append(indices, i)
		}
	}
	return indices
}

// executePodTask runs one Pod task. When mu is non-nil, workspace state mutations
// are serialized for parallel execution.
func (e *Engine) executePodTask(
	ctx context.Context,
	ws *state.WorkflowState,
	snap *customization.Snapshot,
	p persona.Persona,
	basePacket state.HandoffPacket,
	taskIndex int,
	mu *sync.Mutex,
) error {
	lock := func(fn func()) {
		if mu != nil {
			mu.Lock()
			defer mu.Unlock()
		}
		fn()
	}

	t := &ws.Tasks[taskIndex]

	lock(func() {
		ws.Execution.CurrentPersona = state.PersonaPod
		ws.Execution.ActiveTaskID = t.ID
		ws.Execution.ActiveTaskTitle = t.Title
		t.Status = state.TaskStatusRunning
	})

	if err := e.store.SaveWorkflow(ctx, ws); err != nil {
		return fmt.Errorf("save before pod task %s: %w", t.ID[:8], err)
	}

	startEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventTaskStarted, state.PersonaPod,
		events.TaskStartedPayload{TaskID: t.ID, Title: t.Title})
	_ = e.store.AppendEvents(ctx, startEvt)

	packet := basePacket
	lock(func() {
		packet = e.buildPacket(ws, state.PersonaPod, snap)
		packet.Tasks = []state.Task{*t}
	})

	taskStart := time.Now()
	taskCtx, taskCancel := context.WithTimeout(ctx, e.opts.HandoffTimeout)
	out, err := p.Execute(taskCtx, packet)
	taskElapsed := time.Since(taskStart)
	taskCancel()

	if controlErr := e.checkControlState(ctx, ws); controlErr != nil {
		return controlErr
	}

	if err != nil {
		lock(func() {
			t.Status = state.TaskStatusFailed
			ws.Execution.ActiveTaskID = ""
			ws.Execution.ActiveTaskTitle = ""
		})
		failEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
			events.EventTaskFailed, state.PersonaPod,
			events.TaskFailedPayload{TaskID: t.ID, Title: t.Title, Error: err.Error()})
		_ = e.store.AppendEvents(ctx, failEvt)
		_ = e.store.SaveWorkflow(ctx, ws)
		return fmt.Errorf("pod task %s: %w", t.ID[:8], err)
	}

	now := time.Now().UTC()
	lock(func() {
		t.Status = state.TaskStatusCompleted
		t.CompletedAt = &now
		e.refreshPodTaskReadiness(ws)
		for _, art := range out.Artifacts {
			ws.Artifacts = mergeOrAppendArtifact(ws.Mode, ws.Artifacts, art)
		}
		if ws.Summaries == nil {
			ws.Summaries = make(map[state.PersonaKind]string)
		}
		shortID := t.ID
		if len(shortID) > 8 {
			shortID = shortID[:8]
		}
		ws.Summaries[state.PersonaPod] += fmt.Sprintf("[%s] %s\n", shortID, out.Summary)
		ws.Execution.ActiveTaskID = ""
		ws.Execution.ActiveTaskTitle = ""
	})

	for _, art := range out.Artifacts {
		artEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
			events.EventArtifactProduced, state.PersonaPod,
			events.ArtifactProducedPayload{
				TaskID:        t.ID,
				ArtifactName:  art.Name,
				Kind:          string(art.Kind),
				ContentLength: len(art.Content),
			})
		_ = e.store.AppendEvents(ctx, artEvt)
		if ws.Execution.Workspace != nil {
			if writeErr := e.writeArtifactToWorkspace(ws, art); writeErr != nil {
				logger.Warn("engine: failed to flush artifact to workspace",
					zap.String("workflow_id", ws.ID),
					zap.String("task_id", t.ID),
					zap.String("artifact_name", art.Name),
					zap.Error(writeErr),
				)
			}
		}
	}

	doneEvt, _ := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventTaskCompleted, state.PersonaPod,
		events.TaskCompletedPayload{
			TaskID:     t.ID,
			Title:      t.Title,
			Summary:    out.Summary,
			DurationMs: taskElapsed.Milliseconds(),
		})
	_ = e.store.AppendEvents(ctx, doneEvt)
	return e.store.SaveWorkflow(ctx, ws)
}

func (e *Engine) runPodTaskBatch(
	ctx context.Context,
	ws *state.WorkflowState,
	snap *customization.Snapshot,
	p persona.Persona,
	basePacket state.HandoffPacket,
	indices []int,
) error {
	concurrency := e.podConcurrency()
	if concurrency <= 1 || len(indices) <= 1 {
		for _, i := range indices {
			if err := e.executePodTask(ctx, ws, snap, p, basePacket, i, nil); err != nil {
				return err
			}
		}
		return nil
	}

	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	sem := make(chan struct{}, concurrency)
	for _, idx := range indices {
		idx := idx
		g.Go(func() error {
			sem <- struct{}{}
			defer func() { <-sem }()
			return e.executePodTask(gctx, ws, snap, p, basePacket, idx, &mu)
		})
	}
	return g.Wait()
}
