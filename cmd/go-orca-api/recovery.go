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

const interruptedWorkflowError = "workflow interrupted while the server was unavailable; resume to continue"

func reconcileInterruptedWorkflows(ctx context.Context, store storage.Store, log *zap.Logger) (int, error) {
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
				if err := markWorkflowInterrupted(ctx, store, workflow, log); err != nil {
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

func markWorkflowInterrupted(ctx context.Context, store storage.Store, ws *state.WorkflowState, log *zap.Logger) error {
	if ws.Status != state.WorkflowStatusRunning {
		return nil
	}
	if log == nil {
		log = zap.NewNop()
	}

	now := time.Now().UTC()
	previousStatus := ws.Status
	runningTasks := make([]state.Task, 0, len(ws.Tasks))
	for index := range ws.Tasks {
		if ws.Tasks[index].Status != state.TaskStatusRunning {
			continue
		}
		ws.Tasks[index].Status = state.TaskStatusFailed
		ws.Tasks[index].CompletedAt = &now
		runningTasks = append(runningTasks, ws.Tasks[index])
	}

	currentPersona := ws.Execution.CurrentPersona
	ws.Status = state.WorkflowStatusFailed
	ws.ErrorMessage = interruptedWorkflowError
	ws.Execution.CurrentPersona = ""
	ws.Execution.ActiveTaskID = ""
	ws.Execution.ActiveTaskTitle = ""
	ws.UpdatedAt = now
	ws.CompletedAt = &now

	if err := store.SaveWorkflow(ctx, ws); err != nil {
		return fmt.Errorf("save reconciled workflow: %w", err)
	}

	eventsToAppend := make([]*events.Event, 0, len(runningTasks)+2)
	for _, task := range runningTasks {
		evt, err := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
			events.EventTaskFailed, state.PersonaPod,
			events.TaskFailedPayload{TaskID: task.ID, Title: task.Title, Error: interruptedWorkflowError})
		if err != nil {
			return fmt.Errorf("build task failure event: %w", err)
		}
		eventsToAppend = append(eventsToAppend, evt)
	}

	if currentPersona != "" {
		evt, err := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
			events.EventPersonaFailed, currentPersona,
			events.PersonaFailedPayload{Persona: currentPersona, Error: interruptedWorkflowError})
		if err != nil {
			return fmt.Errorf("build persona failure event: %w", err)
		}
		eventsToAppend = append(eventsToAppend, evt)
	}

	transitionEvt, err := events.NewEvent(ws.ID, ws.TenantID, ws.ScopeID,
		events.EventStateTransition, "",
		events.StateTransitionPayload{From: previousStatus, To: ws.Status})
	if err != nil {
		return fmt.Errorf("build state transition event: %w", err)
	}
	eventsToAppend = append(eventsToAppend, transitionEvt)

	if err := store.AppendEvents(ctx, eventsToAppend...); err != nil {
		log.Warn("failed to append recovery events",
			zap.String("workflow_id", ws.ID),
			zap.Error(err),
		)
	}

	log.Info("reconciled interrupted workflow",
		zap.String("workflow_id", ws.ID),
		zap.String("tenant_id", ws.TenantID),
		zap.String("scope_id", ws.ScopeID),
	)

	return nil
}
