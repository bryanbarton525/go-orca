package main

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/go-orca/go-orca/internal/events"
	"github.com/go-orca/go-orca/internal/state"
	"github.com/go-orca/go-orca/internal/storage"
)

const interruptedWorkflowMessage = "workflow paused during API restart; resume or wait for automatic re-queue"

// workflowEnqueuer re-schedules workflows after an API restart.
type workflowEnqueuer interface {
	Enqueue(workflowID string) error
}

func reconcileInterruptedWorkflows(ctx context.Context, store storage.Store, sched workflowEnqueuer, log *zap.Logger) (int, error) {
	if log == nil {
		log = zap.NewNop()
	}

	tenants, err := store.ListTenants(ctx)
	if err != nil {
		return 0, fmt.Errorf("list tenants for workflow recovery: %w", err)
	}

	const pageSize = 100
	recovered := 0
	for _, tenant := range tenants {
		for offset := 0; ; offset += pageSize {
			workflows, err := store.ListWorkflows(ctx, tenant.ID, pageSize, offset)
			if err != nil {
				return recovered, fmt.Errorf("list workflows for tenant %s: %w", tenant.ID, err)
			}
			for _, workflow := range workflows {
				if workflow.Status != state.WorkflowStatusRunning {
					continue
				}
				if err := pauseAndRequeueInterruptedWorkflow(ctx, store, sched, workflow, log); err != nil {
					return recovered, fmt.Errorf("recover workflow %s: %w", workflow.ID, err)
				}
				recovered++
			}
			if len(workflows) < pageSize {
				break
			}
		}
	}

	return recovered, nil
}

func pauseAndRequeueInterruptedWorkflow(ctx context.Context, store storage.Store, sched workflowEnqueuer, ws *state.WorkflowState, log *zap.Logger) error {
	if ws.Status != state.WorkflowStatusRunning {
		return nil
	}
	if log == nil {
		log = zap.NewNop()
	}

	now := time.Now().UTC()
	previousStatus := ws.Status

	for index := range ws.Tasks {
		if ws.Tasks[index].Status == state.TaskStatusRunning {
			ws.Tasks[index].Status = state.TaskStatusPending
			ws.Tasks[index].CompletedAt = nil
		}
	}

	ws.Status = state.WorkflowStatusPaused
	ws.ErrorMessage = interruptedWorkflowMessage
	ws.Execution.CurrentPersona = ""
	ws.Execution.ActiveTaskID = ""
	ws.Execution.ActiveTaskTitle = ""
	ws.UpdatedAt = now
	ws.CompletedAt = nil

	if err := store.SaveWorkflow(ctx, ws); err != nil {
		return fmt.Errorf("save reconciled workflow: %w", err)
	}

	pausedEvt, err := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventWorkflowPaused, "",
		events.StateTransitionPayload{From: previousStatus, To: ws.Status})
	if err != nil {
		return fmt.Errorf("build workflow paused event: %w", err)
	}
	transitionEvt, err := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventStateTransition, "",
		events.StateTransitionPayload{From: previousStatus, To: ws.Status})
	if err != nil {
		return fmt.Errorf("build state transition event: %w", err)
	}
	if err := store.AppendEvents(ctx, pausedEvt, transitionEvt); err != nil {
		log.Warn("failed to append recovery events",
			zap.String("workflow_id", ws.ID),
			zap.Error(err),
		)
	}

	if sched == nil {
		log.Warn("scheduler unavailable; workflow left paused after restart",
			zap.String("workflow_id", ws.ID),
		)
		return nil
	}

	if err := store.UpdateWorkflowStatus(ctx, ws.ID, state.WorkflowStatusPending, ""); err != nil {
		return fmt.Errorf("re-queue workflow: %w", err)
	}
	if err := sched.Enqueue(ws.ID); err != nil {
		return fmt.Errorf("enqueue recovered workflow: %w", err)
	}

	resumedEvt, err := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventWorkflowResumed, "",
		events.StateTransitionPayload{From: state.WorkflowStatusPaused, To: state.WorkflowStatusPending})
	if err == nil {
		_ = store.AppendEvents(ctx, resumedEvt)
	}

	log.Info("re-queued interrupted workflow after restart",
		zap.String("workflow_id", ws.ID),
		zap.String("tenant_id", ws.TenantID),
		zap.String("scope_id", ws.ScopeID),
	)

	return nil
}
