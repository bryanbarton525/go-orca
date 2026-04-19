"use client";

import { useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Bot,
  BrainCircuit,
  Boxes,
  CheckCircle2,
  Cpu,
  Play,
  Radio,
  RotateCcw,
  Search,
  ShieldCheck,
  Sparkles,
  Square,
  TriangleAlert,
  WandSparkles,
} from "lucide-react";
import { useOrcaWorkspace } from "./orca-workspace-provider";
import {
  buildWorkflowStreamUrl,
  cancelWorkflow,
  createWorkflow,
  getWorkflow,
  getWorkflowEvents,
  listProviderModels,
  listProviders,
  listWorkflows,
  resumeWorkflow,
} from "../lib/orca/api";
import { formatDate, formatRelative, prettyJson, workflowModes, deliveryActions } from "../lib/orca/presentation";
import type { Artifact, CreateWorkflowRequest, EventRecord, Task, WorkflowState } from "../types/orca";
import {
  EmptyState,
  InputLabel,
  SectionIntro,
  StatusBadge,
  Surface,
  primaryButtonClassName,
  secondaryButtonClassName,
  textFieldClassName,
} from "./ui";

const workflowPhases = [
  { id: "director", label: "Director", caption: "Intent routing" },
  { id: "project manager", label: "Project Manager", caption: "Requirements cut" },
  { id: "architect", label: "Architect", caption: "Plan and design" },
  { id: "implementer", label: "Implementer", caption: "Artifact execution" },
  { id: "qa", label: "QA", caption: "Validation loop" },
  { id: "finalizer", label: "Finalizer", caption: "Delivery handoff" },
  { id: "refiner", label: "Refiner", caption: "Improvement pass" },
] as const;

const workflowVisualizationCardClassName =
  "min-w-0 overflow-hidden rounded-[1.25rem] border border-shell-border/15 bg-shell-panel/72 p-4 backdrop-blur-sm";

const workflowVisualizationLabelClassName =
  "text-[0.68rem] font-semibold uppercase tracking-[0.2em] text-shell-soft";

const workflowVisualizationValueClassName = "mt-3 text-2xl font-semibold text-ink [overflow-wrap:anywhere]";
const interruptedWorkflowErrorSnippet = "workflow interrupted while the server was unavailable";

type PhaseState = "complete" | "active" | "pending";

type WorkflowExplorerSelection =
  | {
      kind: "persona";
      id: string;
    }
  | {
      kind: "object";
      id: string;
    };

const workflowTerminalStatuses = new Set(["completed", "cancelled", "failed"]);

function workflowLabel(workflow?: Pick<WorkflowState, "id" | "title" | "request">) {
  return workflow?.title || workflow?.request || workflow?.id || "Unlabelled workflow";
}

function isWorkflowTerminal(status?: string) {
  return workflowTerminalStatuses.has(status ?? "");
}

function isInterruptedWorkflow(workflow?: Pick<WorkflowState, "status" | "error_message">) {
  return (
    workflow?.status === "failed" &&
    workflow.error_message?.toLowerCase().includes(interruptedWorkflowErrorSnippet)
  );
}

function workflowStatusLabel(workflow?: Pick<WorkflowState, "status" | "error_message">) {
  if (!workflow?.status) {
    return "unknown";
  }

  return isInterruptedWorkflow(workflow) ? "interrupted" : workflow.status;
}

function hasLingeringExecutionState(workflow?: WorkflowState) {
  return Boolean(
    workflow?.execution?.current_persona ||
      workflow?.execution?.active_task_id ||
      workflow?.execution?.active_task_title
  );
}

function shouldRefreshWorkflowSnapshot(workflow?: WorkflowState) {
  if (!workflow) {
    return false;
  }

  return !isWorkflowTerminal(workflow.status) || hasLingeringExecutionState(workflow);
}

function workflowCurrentPersonaLabel(workflow?: WorkflowState) {
  if (workflow?.execution?.current_persona) {
    return workflow.execution.current_persona;
  }

  return workflow && isWorkflowTerminal(workflow.status) ? "No active persona" : "Awaiting dispatch";
}

function workflowActiveTaskLabel(workflow?: WorkflowState) {
  return workflow?.execution?.active_task_title || workflow?.execution?.active_task_id || "No active task";
}

function summarizeText(value?: string, limit = 144) {
  if (!value) {
    return "No request summary was persisted on this workflow.";
  }

  if (value.length <= limit) {
    return value;
  }

  return `${value.slice(0, limit - 1)}…`;
}

function isTaskComplete(task: Task) {
  const normalizedStatus = (task.status ?? "").toLowerCase();

  if (normalizedStatus === "completed" || normalizedStatus === "done") {
    return true;
  }

  return !normalizedStatus && Boolean(task.completed_at);
}

function completedTaskCount(tasks?: Task[]) {
  return tasks?.filter(isTaskComplete).length ?? 0;
}

function normalizePersonaId(value?: string) {
  return (value ?? "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
}

function contentPreview(value?: string, limit = 220) {
  if (!value) {
    return null;
  }

  if (value.length <= limit) {
    return value;
  }

  return `${value.slice(0, limit - 1)}…`;
}

function eventTimestamp(event: EventRecord) {
  const timestamp = event.occurred_at ?? event.created_at;
  if (!timestamp) {
    return 0;
  }

  const parsed = new Date(timestamp).getTime();
  return Number.isNaN(parsed) ? 0 : parsed;
}

function eventIdentity(event: EventRecord) {
  return (
    event.id ||
    `${event.type ?? "event"}:${event.persona ?? "system"}:${event.occurred_at ?? event.created_at ?? ""}:${prettyJson(
      event.payload ?? null
    )}`
  );
}

function mergeLiveFeedEvents(current: EventRecord[], journal: EventRecord[]) {
  const merged = new Map<string, EventRecord>();

  for (const event of [...current, ...journal.slice(-30).reverse()]) {
    const key = eventIdentity(event);
    if (!merged.has(key)) {
      merged.set(key, event);
    }
  }

  return Array.from(merged.values())
    .sort((left, right) => eventTimestamp(right) - eventTimestamp(left))
    .slice(0, 30);
}

function nonEmptyEntries(value: Record<string, unknown> | null | undefined) {
  if (!value) {
    return [];
  }

  return Object.entries(value).filter(([, entry]) => {
    if (entry === null || entry === undefined) {
      return false;
    }

    if (Array.isArray(entry)) {
      return entry.length > 0;
    }

    if (typeof entry === "object") {
      return Object.keys(entry).length > 0;
    }

    if (typeof entry === "string") {
      return entry.trim().length > 0;
    }

    return true;
  });
}

function artifactLabel(artifact: Artifact) {
  return artifact.name || artifact.path || artifact.kind || artifact.id;
}

function currentPhaseIndex(workflow?: WorkflowState) {
  const activePersona = normalizePersonaId(workflow?.execution?.current_persona);
  if (!activePersona) {
    return -1;
  }

  return workflowPhases.findIndex((phase) => activePersona.includes(normalizePersonaId(phase.id)));
}

function phaseStateFor(workflow: WorkflowState, phaseIndex: number): PhaseState {
  const activeIndex = currentPhaseIndex(workflow);
  const terminal = isWorkflowTerminal(workflow.status);

  if (terminal) {
    if (normalizePersonaId(workflowPhases[phaseIndex]?.id) === "refiner") {
      return (workflow.all_suggestions?.length ?? 0) > 0 ? "complete" : "pending";
    }

    if (activeIndex >= 0) {
      return phaseIndex <= activeIndex ? "complete" : "pending";
    }

    return "complete";
  }

  if (activeIndex === -1) {
    return phaseIndex === 0 ? "active" : "pending";
  }

  if (phaseIndex < activeIndex) {
    return "complete";
  }

  if (phaseIndex === activeIndex) {
    return "active";
  }

  return "pending";
}

function phaseCardClassName(state: PhaseState) {
  if (state === "complete") {
    return "border-shell-success/35 bg-[linear-gradient(135deg,rgb(var(--color-shell-success)/0.18),rgb(var(--color-shell-panel)/0.92))] text-ink shadow-[0_16px_36px_rgb(var(--color-shell-success)/0.14)]";
  }

  if (state === "active") {
    return "border-lagoon/45 bg-[linear-gradient(135deg,rgb(var(--color-lagoon)/0.2),rgb(var(--color-ember)/0.12))] text-ink shadow-[0_18px_40px_rgb(var(--color-lagoon)/0.16)]";
  }

  return "border-shell-border/15 bg-shell-panel/72 text-ink";
}

function phaseIconClassName(state: PhaseState) {
  if (state === "complete") {
    return "text-shell-success";
  }

  if (state === "active") {
    return "text-lagoon";
  }

  return "text-shell-soft";
}

function LiveEventList({
  events,
  emptyTitle,
  emptyBody,
}: {
  events: EventRecord[];
  emptyTitle: string;
  emptyBody: string;
}) {
  if (events.length === 0) {
    return <EmptyState title={emptyTitle} body={emptyBody} />;
  }

  return (
    <div className="thin-scrollbar max-h-[34rem] space-y-3 overflow-auto pr-1">
      {events.map((event, index) => (
        <div key={`${event.id}-${index}`} className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
          <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
            <div>
              <p className="text-sm font-semibold text-ink">{event.type ?? "event"}</p>
              <p className="mt-1 text-xs text-shell-soft">{event.persona || "system"}</p>
            </div>
            <span className="text-xs text-shell-soft">{formatDate(event.occurred_at ?? event.created_at)}</span>
          </div>
          <pre className="thin-scrollbar mt-3 overflow-x-auto rounded-2xl bg-shell-code p-3 text-xs leading-6 text-shell-code-text">
            {prettyJson(event.payload ?? event)}
          </pre>
        </div>
      ))}
    </div>
  );
}

function WorkflowPhaseCard({
  label,
  caption,
  state,
  selected = false,
  pulse = false,
  onSelect,
}: {
  label: string;
  caption: string;
  state: PhaseState;
  selected?: boolean;
  pulse?: boolean;
  onSelect?: () => void;
}) {
  const className = `relative overflow-hidden rounded-[1.35rem] border px-4 py-3 text-left transition ${phaseCardClassName(state)} ${
    selected ? "ring-2 ring-lagoon/55 ring-offset-2 ring-offset-shell-panel/10" : ""
  }`;

  const statusIcon =
    state === "complete" ? (
      <CheckCircle2 className={`h-4 w-4 shrink-0 ${phaseIconClassName(state)}`} />
    ) : state === "active" ? (
      <div className="flex shrink-0 items-center gap-2">
        {pulse ? (
          <span className="relative flex h-2.5 w-2.5 shrink-0">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-lagoon/60" />
            <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-lagoon" />
          </span>
        ) : null}
        <Sparkles className={`h-4 w-4 shrink-0 ${phaseIconClassName(state)}`} />
      </div>
    ) : (
      <Bot className={`h-4 w-4 shrink-0 ${phaseIconClassName(state)}`} />
    );

  if (onSelect) {
    return (
      <button type="button" onClick={onSelect} className={className}>
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <p className="text-sm font-semibold [overflow-wrap:anywhere]">{label}</p>
            <p className={`mt-1 text-xs [overflow-wrap:anywhere] ${state === "pending" ? "text-shell-soft" : "text-shell-muted"}`}>{caption}</p>
          </div>
          {statusIcon}
        </div>
      </button>
    );
  }

  return (
    <div className={className}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-sm font-semibold [overflow-wrap:anywhere]">{label}</p>
          <p className={`mt-1 text-xs [overflow-wrap:anywhere] ${state === "pending" ? "text-shell-soft" : "text-shell-muted"}`}>{caption}</p>
        </div>
        {statusIcon}
      </div>
    </div>
  );
}

function WorkflowVisualization({
  workflow,
  selectedPersonaId,
  onSelectPersona,
  onSelectObject,
}: {
  workflow: WorkflowState;
  selectedPersonaId?: string;
  onSelectPersona?: (personaId: string) => void;
  onSelectObject?: (objectId: string) => void;
}) {
  const planningPhases = workflowPhases.slice(0, 3);
  const executionPhases = workflowPhases.slice(3);
  const taskTotal = workflow.tasks?.length ?? 0;
  const taskDone = completedTaskCount(workflow.tasks);
  const artifactTotal = workflow.artifacts?.length ?? 0;
  const suggestionTotal = workflow.all_suggestions?.length ?? 0;
  const blockingIssueTotal = workflow.blocking_issues?.length ?? 0;
  const activePersona = workflowCurrentPersonaLabel(workflow);
  const activeTask = workflowActiveTaskLabel(workflow);
  const activePersonaId = normalizePersonaId(workflow.execution?.current_persona);
  const activeWorkflow = !isWorkflowTerminal(workflow.status);
  const providerRoute = workflow.provider_name || "unassigned";
  const modelRoute = workflow.model_name || "auto-select";
  const phaseProgress = (() => {
    const idx = currentPhaseIndex(workflow);
    if (idx < 0) {
      return 0;
    }

    if (isWorkflowTerminal(workflow.status)) {
      const completedPhases = workflowPhases.filter((_, i) => phaseStateFor(workflow, i) === "complete").length;
      return Math.round((completedPhases / workflowPhases.length) * 100);
    }

    return Math.round(((idx + 1) / workflowPhases.length) * 100);
  })();
  const progress = taskTotal > 0
    ? Math.round((taskDone / taskTotal) * 100)
    : workflow.status === "completed"
      ? 100
      : isWorkflowTerminal(workflow.status)
        ? phaseProgress
        : Math.max(phaseProgress, 0);
  const displayStatus = workflowStatusLabel(workflow);

  return (
    <div className="relative overflow-hidden rounded-[1.9rem] border border-shell-border/20 bg-[linear-gradient(180deg,rgb(var(--color-shell-panel)/0.96),rgb(var(--color-shell-subtle)/0.92))] p-5 text-ink shadow-aura">
      <div className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_top_left,rgb(var(--color-lagoon)/0.16),transparent_34%),radial-gradient(circle_at_bottom_right,rgb(var(--color-ember)/0.14),transparent_30%)]" />
      <div className="pointer-events-none absolute inset-x-10 top-[4.9rem] hidden h-px bg-[linear-gradient(90deg,transparent,rgb(var(--color-lagoon)/0.35),transparent)] xl:block" />
      <div className="relative space-y-5">
        <div className="flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
          <div className="space-y-2">
            <p className="text-[0.72rem] font-semibold uppercase tracking-[0.26em] text-lagoon">Workflow Visualization</p>
            <h3 className="max-w-3xl font-display text-2xl font-semibold tracking-tight text-ink sm:text-3xl">
              {workflowLabel(workflow)}
            </h3>
            <p className="max-w-3xl text-sm leading-6 text-shell-muted">{summarizeText(workflow.request)}</p>
          </div>
          <div className="rounded-full border border-shell-border/20 bg-shell-panel/72 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.18em] text-shell-muted">
            {displayStatus}
          </div>
        </div>

        <div className="grid gap-5 xl:grid-cols-[220px_minmax(0,1fr)_220px] xl:items-start">
          <div className="space-y-3">
            <p className={workflowVisualizationLabelClassName}>Planning Personas</p>
            {planningPhases.map((phase, index) => (
              <WorkflowPhaseCard
                key={phase.id}
                label={phase.label}
                caption={phase.caption}
                state={phaseStateFor(workflow, index)}
                selected={selectedPersonaId === normalizePersonaId(phase.id)}
                pulse={activeWorkflow && activePersonaId === normalizePersonaId(phase.id)}
                onSelect={onSelectPersona ? () => onSelectPersona(normalizePersonaId(phase.id)) : undefined}
              />
            ))}
          </div>

          <div className="rounded-[1.75rem] border border-shell-border/15 bg-[linear-gradient(180deg,rgb(var(--color-shell-panel)/0.72),rgb(var(--color-shell-subtle)/0.92))] p-5 backdrop-blur-sm">
            <div className="flex min-w-0 flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
              <div className="min-w-0">
                <p className="text-[0.68rem] font-semibold uppercase tracking-[0.22em] text-lagoon">Orchestration Plane</p>
                <p className="mt-2 text-xl font-semibold text-ink [overflow-wrap:anywhere]">{activePersona}</p>
                <div className="mt-3 flex flex-wrap items-center gap-2 text-sm text-shell-muted">
                  {activeWorkflow ? (
                    <span className="inline-flex items-center gap-2 rounded-full border border-lagoon/30 bg-lagoon/10 px-3 py-1 text-[0.68rem] font-semibold uppercase tracking-[0.16em] text-lagoon">
                      <span className="relative flex h-2.5 w-2.5 shrink-0">
                        <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-lagoon/60" />
                        <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-lagoon" />
                      </span>
                      Live step
                    </span>
                  ) : null}
                  <span className="min-w-0 flex-1 [overflow-wrap:anywhere]">{activeTask}</span>
                </div>
              </div>
              <div className="grid gap-2 sm:text-right">
                <p className="text-[0.68rem] font-semibold uppercase tracking-[0.22em] text-shell-soft">Runtime</p>
                <p className="text-sm text-shell-muted [overflow-wrap:anywhere]">{workflow.mode ?? "mode pending"}</p>
                <p className="text-sm text-shell-muted">QA cycle {workflow.execution?.qa_cycle ?? 0}</p>
              </div>
            </div>

            <div className="mt-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
              <button type="button" onClick={() => onSelectObject?.("tasks")} className={`${workflowVisualizationCardClassName} text-left transition hover:border-lagoon/40 ${onSelectObject ? "cursor-pointer" : ""}`}>
                <div className="flex items-center justify-between">
                  <p className={workflowVisualizationLabelClassName}>Tasks</p>
                  <BrainCircuit className="h-4 w-4 text-lagoon" />
                </div>
                <p className={workflowVisualizationValueClassName}><span className="tabular-nums">{taskDone}</span><span className="text-shell-soft">/</span><span className="tabular-nums">{taskTotal}</span></p>
              </button>
              <button type="button" onClick={() => onSelectObject?.("artifacts")} className={`${workflowVisualizationCardClassName} text-left transition hover:border-lagoon/40 ${onSelectObject ? "cursor-pointer" : ""}`}>
                <div className="flex items-center justify-between">
                  <p className={workflowVisualizationLabelClassName}>Artifacts</p>
                  <Boxes className="h-4 w-4 text-lagoon" />
                </div>
                <p className={workflowVisualizationValueClassName}><span className="tabular-nums">{artifactTotal}</span></p>
              </button>
              <button type="button" onClick={() => onSelectObject?.("suggestions")} className={`${workflowVisualizationCardClassName} text-left transition hover:border-lagoon/40 ${onSelectObject ? "cursor-pointer" : ""}`}>
                <div className="flex items-center justify-between">
                  <p className={workflowVisualizationLabelClassName}>Suggestions</p>
                  <Sparkles className="h-4 w-4 text-shell-success" />
                </div>
                <p className={workflowVisualizationValueClassName}><span className="tabular-nums">{suggestionTotal}</span></p>
              </button>
              <button type="button" onClick={() => onSelectObject?.("blocking")} className={`${workflowVisualizationCardClassName} text-left transition hover:border-lagoon/40 ${onSelectObject ? "cursor-pointer" : ""}`}>
                <div className="flex items-center justify-between">
                  <p className={workflowVisualizationLabelClassName}>Blocking</p>
                  <TriangleAlert className="h-4 w-4 text-shell-danger" />
                </div>
                <p className={workflowVisualizationValueClassName}><span className="tabular-nums">{blockingIssueTotal}</span></p>
              </button>
            </div>

            <div className="mt-5 rounded-full border border-shell-border/15 bg-shell-panel/72 px-4 py-3 backdrop-blur-sm">
              <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <p className="text-[0.72rem] font-semibold uppercase tracking-[0.24em] text-shell-soft">Task progress and live routing</p>
                <p className="text-sm text-shell-muted">{progress}% complete</p>
              </div>
              <div className="mt-3 h-2 rounded-full bg-shell-border/12">
                <div className="h-full rounded-full bg-[linear-gradient(90deg,rgb(var(--color-lagoon)),rgb(var(--color-ember)))]" style={{ width: `${progress}%` }} />
              </div>
            </div>

            <div className="mt-5 grid gap-3 md:grid-cols-2">
              <div className={workflowVisualizationCardClassName}>
                <div className="flex items-center gap-3 text-lagoon">
                  <Cpu className="h-4 w-4" />
                  <p className="text-[0.68rem] font-semibold uppercase tracking-[0.2em] text-shell-soft">Model route</p>
                </div>
                <p className="mt-3 text-sm text-ink">{providerRoute}</p>
                <p className="mt-1 text-xs text-shell-soft">{modelRoute}</p>
              </div>
              <div className={workflowVisualizationCardClassName}>
                <div className="flex items-center gap-3 text-ember">
                  <ShieldCheck className="h-4 w-4" />
                  <p className="text-[0.68rem] font-semibold uppercase tracking-[0.2em] text-shell-soft">Scope routing</p>
                </div>
                <p className="mt-3 break-all text-sm text-ink">{workflow.tenant_id ?? "server default"}</p>
                <p className="mt-1 break-all text-xs text-shell-soft">{workflow.scope_id ?? "server default scope"}</p>
              </div>
            </div>
          </div>

          <div className="space-y-3">
            <p className={workflowVisualizationLabelClassName}>Execution Personas</p>
            {executionPhases.map((phase, index) => (
              <WorkflowPhaseCard
                key={phase.id}
                label={phase.label}
                caption={phase.caption}
                state={phaseStateFor(workflow, planningPhases.length + index)}
                selected={selectedPersonaId === normalizePersonaId(phase.id)}
                pulse={activeWorkflow && activePersonaId === normalizePersonaId(phase.id)}
                onSelect={onSelectPersona ? () => onSelectPersona(normalizePersonaId(phase.id)) : undefined}
              />
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function WorkflowExplorer({
  workflow,
  events,
  selection,
  onSelect,
}: {
  workflow: WorkflowState;
  events: EventRecord[];
  selection: WorkflowExplorerSelection;
  onSelect: (selection: WorkflowExplorerSelection) => void;
}) {
  const personaDetails = useMemo(
    () =>
      workflowPhases.map((phase, index) => {
        const personaId = normalizePersonaId(phase.id);
        return {
          id: personaId,
          label: phase.label,
          caption: phase.caption,
          state: phaseStateFor(workflow, index),
          summary: workflow.summaries?.[personaId],
          tasks: (workflow.tasks ?? []).filter((task) => normalizePersonaId(task.assigned_to) === personaId),
          artifacts: (workflow.artifacts ?? []).filter((artifact) => normalizePersonaId(artifact.created_by) === personaId),
          events: events.filter((event) => normalizePersonaId(event.persona) === personaId),
          active: normalizePersonaId(workflow.execution?.current_persona) === personaId,
        };
      }),
    [events, workflow]
  );

  const objectCards = useMemo(
    () => [
      {
        id: "constitution",
        label: "Constitution",
        caption: "Vision, goals, and constraints",
        count: nonEmptyEntries(workflow.constitution as Record<string, unknown> | null | undefined).length,
      },
      {
        id: "requirements",
        label: "Requirements",
        caption: "Functional and non-functional scope",
        count:
          (workflow.requirements?.functional?.length ?? 0) +
          (workflow.requirements?.non_functional?.length ?? 0) +
          (workflow.requirements?.dependencies?.length ?? 0),
      },
      {
        id: "design",
        label: "Design",
        caption: "Architecture and delivery plan",
        count:
          (workflow.design?.components?.length ?? 0) +
          (workflow.design?.decisions?.length ?? 0) +
          (workflow.design?.tech_stack?.length ?? 0),
      },
      {
        id: "tasks",
        label: "Tasks",
        caption: "Execution graph and outputs",
        count: workflow.tasks?.length ?? 0,
      },
      {
        id: "artifacts",
        label: "Artifacts",
        caption: "Generated deliverables",
        count: workflow.artifacts?.length ?? 0,
      },
      {
        id: "summaries",
        label: "Summaries",
        caption: "Persona handoff notes",
        count: Object.keys(workflow.summaries ?? {}).length,
      },
      {
        id: "finalization",
        label: "Finalization",
        caption: "Delivery result and links",
        count:
          (workflow.finalization?.links?.length ?? 0) +
          (workflow.finalization?.suggestions?.length ?? 0) +
          (workflow.finalization?.summary ? 1 : 0),
      },
      {
        id: "blocking",
        label: "Blocking",
        caption: "Issues stopping the run",
        count: workflow.blocking_issues?.length ?? 0,
      },
      {
        id: "suggestions",
        label: "Suggestions",
        caption: "Refinement ideas and follow-up",
        count: workflow.all_suggestions?.length ?? 0,
      },
    ],
    [workflow]
  );

  const selectedPersona = selection.kind === "persona" ? personaDetails.find((persona) => persona.id === selection.id) ?? null : null;
  const selectedObject = selection.kind === "object" ? objectCards.find((objectCard) => objectCard.id === selection.id) ?? null : null;

  return (
    <div className="space-y-5">
      <div>
        <p className="eyebrow">Interactive Drill-Down</p>
        <h2 className="mt-2 font-display text-2xl font-semibold text-ink">Personas and workflow objects</h2>
        <p className="mt-2 max-w-3xl text-sm leading-6 text-shell-muted">
          Select a persona to inspect its summary, outputs, and event trail, or pick a workflow object to inspect the persisted document behind the run.
        </p>
      </div>

      <div className="grid gap-5 2xl:grid-cols-[minmax(0,0.92fr)_minmax(0,1.08fr)] 2xl:items-start">
        <div className="space-y-4">
          <div className="space-y-3">
            <p className="text-xs font-semibold uppercase tracking-[0.18em] text-shell-soft">Personas</p>
            <div className="grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(15rem,1fr))]">
              {personaDetails.map((persona) => (
                <button
                  key={persona.id}
                  type="button"
                  onClick={() => onSelect({ kind: "persona", id: persona.id })}
                  className={`rounded-3xl border p-4 text-left transition ${
                    selection.kind === "persona" && selection.id === persona.id
                      ? "border-lagoon bg-lagoon/12 shadow-[0_14px_36px_rgb(var(--color-lagoon)/0.12)]"
                      : "border-shell-border/40 bg-shell-panel/80 hover:border-lagoon"
                  }`}
                >
                  <div className="flex flex-wrap items-start justify-between gap-2">
                    <div className="min-w-0 flex-1">
                      <p className="text-sm font-semibold text-ink">{persona.label}</p>
                      <p className="mt-1 text-xs text-shell-soft">{persona.caption}</p>
                    </div>
                    <StatusBadge status={persona.state} />
                  </div>
                  <div className="mt-3 grid grid-cols-3 gap-2 text-xs text-shell-muted">
                    <span>{persona.tasks.length} tasks</span>
                    <span>{persona.artifacts.length} artifacts</span>
                    <span>{persona.events.length} events</span>
                  </div>
                </button>
              ))}
            </div>
          </div>

          <div className="space-y-3">
            <p className="text-xs font-semibold uppercase tracking-[0.18em] text-shell-soft">Workflow objects</p>
            <div className="grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(15rem,1fr))]">
              {objectCards.map((objectCard) => (
                <button
                  key={objectCard.id}
                  type="button"
                  onClick={() => onSelect({ kind: "object", id: objectCard.id })}
                  className={`rounded-3xl border p-4 text-left transition ${
                    selection.kind === "object" && selection.id === objectCard.id
                      ? "border-ember bg-ember/10 shadow-[0_14px_36px_rgb(var(--color-ember)/0.12)]"
                      : "border-shell-border/40 bg-shell-panel/80 hover:border-ember"
                  }`}
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0 flex-1">
                      <p className="text-sm font-semibold text-ink">{objectCard.label}</p>
                      <p className="mt-1 text-xs text-shell-soft">{objectCard.caption}</p>
                    </div>
                    <span className="shrink-0 rounded-full border border-shell-border/35 bg-shell-panel/90 px-2.5 py-1 text-xs font-semibold leading-none text-ink">
                      {objectCard.count}
                    </span>
                  </div>
                </button>
              ))}
            </div>
          </div>
        </div>

        <div className="rounded-[1.75rem] border border-shell-border/40 bg-shell-subtle p-5">
        {selectedPersona ? (
          <div className="space-y-4">
            <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
              <div>
                <p className="eyebrow">Persona Detail</p>
                <h3 className="mt-2 font-display text-2xl font-semibold text-ink">{selectedPersona.label}</h3>
                <p className="mt-2 text-sm leading-6 text-shell-muted">{selectedPersona.caption}</p>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <StatusBadge status={selectedPersona.state} />
                {selectedPersona.active ? (
                  <span className="rounded-full border border-lagoon/35 bg-lagoon/12 px-3 py-1 text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">
                    Active persona
                  </span>
                ) : null}
              </div>
            </div>

            <div className="grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(8.5rem,1fr))]">
              <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Tasks</p>
                <p className="mt-2 text-2xl font-semibold text-ink">{selectedPersona.tasks.length}</p>
              </div>
              <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Artifacts</p>
                <p className="mt-2 text-2xl font-semibold text-ink">{selectedPersona.artifacts.length}</p>
              </div>
              <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Events</p>
                <p className="mt-2 text-2xl font-semibold text-ink">{selectedPersona.events.length}</p>
              </div>
            </div>

            <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
              <p className="text-sm font-semibold text-ink">Summary</p>
              <p className="mt-2 text-sm leading-6 text-shell-muted">
                {selectedPersona.summary || "No explicit summary was persisted for this persona on the selected workflow."}
              </p>
            </div>

            <div className="grid gap-4 [grid-template-columns:repeat(auto-fit,minmax(18rem,1fr))]">
              <div className="min-w-0 space-y-3">
                <p className="text-sm font-semibold text-ink">Task outputs</p>
                {selectedPersona.tasks.length > 0 ? (
                  <div className="thin-scrollbar max-h-[22rem] space-y-3 overflow-auto pr-1">
                    {selectedPersona.tasks.map((task) => (
                      <div key={task.id} className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                        <div className="flex items-start justify-between gap-3">
                          <p className="text-sm font-semibold text-ink">{task.title || task.id}</p>
                          <StatusBadge status={task.status} />
                        </div>
                        <p className="mt-2 text-sm leading-6 text-shell-muted">
                          {task.output || task.description || "No output was persisted for this task."}
                        </p>
                      </div>
                    ))}
                  </div>
                ) : (
                  <EmptyState title="No persona tasks" body="This workflow did not assign persisted tasks directly to this persona." />
                )}
              </div>

              <div className="min-w-0 space-y-3">
                <p className="text-sm font-semibold text-ink">Artifacts and events</p>
                <div className="thin-scrollbar max-h-[22rem] space-y-3 overflow-auto pr-1">
                  {selectedPersona.artifacts.map((artifact) => (
                    <div key={artifact.id} className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                      <p className="text-sm font-semibold text-ink">{artifactLabel(artifact)}</p>
                      <p className="mt-1 text-xs uppercase tracking-[0.16em] text-shell-soft">{artifact.kind || "artifact"}</p>
                      <p className="mt-2 text-sm leading-6 text-shell-muted">
                        {contentPreview(artifact.content) || artifact.description || artifact.path || "No artifact preview available."}
                      </p>
                    </div>
                  ))}

                  {selectedPersona.events.slice(0, 5).map((event, index) => (
                    <div key={`${event.id}-${index}`} className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                      <div className="flex items-start justify-between gap-3">
                        <p className="text-sm font-semibold text-ink">{event.type || "event"}</p>
                        <span className="text-xs text-shell-soft">{formatDate(event.occurred_at ?? event.created_at)}</span>
                      </div>
                      <pre className="thin-scrollbar mt-2 overflow-x-auto rounded-2xl bg-shell-code p-3 text-xs leading-6 text-shell-code-text">
                        {prettyJson(event.payload ?? event)}
                      </pre>
                    </div>
                  ))}

                  {selectedPersona.artifacts.length === 0 && selectedPersona.events.length === 0 ? (
                    <EmptyState title="No persona outputs" body="No artifacts or events were persisted for this persona on the selected workflow." />
                  ) : null}
                </div>
              </div>
            </div>
          </div>
        ) : selectedObject ? (
          <div className="space-y-4">
            <div>
              <p className="eyebrow">Workflow Object</p>
              <h3 className="mt-2 font-display text-2xl font-semibold text-ink">{selectedObject.label}</h3>
              <p className="mt-2 text-sm leading-6 text-shell-muted">{selectedObject.caption}</p>
            </div>

            {selectedObject.id === "constitution" ? (
              <div className="space-y-3">
                {nonEmptyEntries(workflow.constitution as Record<string, unknown> | null | undefined).map(([key, value]) => (
                  <div key={key} className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                    <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">{key.replace(/_/g, " ")}</p>
                    <pre className="thin-scrollbar mt-2 overflow-x-auto rounded-2xl bg-shell-code p-3 text-xs leading-6 text-shell-code-text">
                      {prettyJson(value)}
                    </pre>
                  </div>
                ))}
                {nonEmptyEntries(workflow.constitution as Record<string, unknown> | null | undefined).length === 0 ? (
                  <EmptyState title="No constitution data" body="This workflow did not persist a constitution document." />
                ) : null}
              </div>
            ) : null}

            {selectedObject.id === "requirements" ? (
              <div className="space-y-3">
                <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                  <p className="text-sm font-semibold text-ink">Functional requirements</p>
                  <pre className="thin-scrollbar mt-2 overflow-x-auto rounded-2xl bg-shell-code p-3 text-xs leading-6 text-shell-code-text">
                    {prettyJson(workflow.requirements?.functional ?? [])}
                  </pre>
                </div>
                <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                  <p className="text-sm font-semibold text-ink">Non-functional requirements</p>
                  <pre className="thin-scrollbar mt-2 overflow-x-auto rounded-2xl bg-shell-code p-3 text-xs leading-6 text-shell-code-text">
                    {prettyJson(workflow.requirements?.non_functional ?? [])}
                  </pre>
                </div>
                <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                  <p className="text-sm font-semibold text-ink">Dependencies</p>
                  <pre className="thin-scrollbar mt-2 overflow-x-auto rounded-2xl bg-shell-code p-3 text-xs leading-6 text-shell-code-text">
                    {prettyJson(workflow.requirements?.dependencies ?? [])}
                  </pre>
                </div>
              </div>
            ) : null}

            {selectedObject.id === "design" ? (
              <div className="space-y-3">
                <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                  <p className="text-sm font-semibold text-ink">Overview</p>
                  <p className="mt-2 text-sm leading-6 text-shell-muted">{workflow.design?.overview || "No design overview persisted."}</p>
                </div>
                <div className="grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(16rem,1fr))]">
                  <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                    <p className="text-sm font-semibold text-ink">Components</p>
                    <pre className="thin-scrollbar mt-2 overflow-x-auto rounded-2xl bg-shell-code p-3 text-xs leading-6 text-shell-code-text">
                      {prettyJson(workflow.design?.components ?? [])}
                    </pre>
                  </div>
                  <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                    <p className="text-sm font-semibold text-ink">Decisions</p>
                    <pre className="thin-scrollbar mt-2 overflow-x-auto rounded-2xl bg-shell-code p-3 text-xs leading-6 text-shell-code-text">
                      {prettyJson(workflow.design?.decisions ?? [])}
                    </pre>
                  </div>
                </div>
              </div>
            ) : null}

            {selectedObject.id === "tasks" ? (
              <div className="thin-scrollbar max-h-[28rem] space-y-3 overflow-auto pr-1">
                {(workflow.tasks ?? []).map((task) => (
                  <div key={task.id} className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                    <div className="flex items-start justify-between gap-3">
                      <div>
                        <p className="text-sm font-semibold text-ink">{task.title || task.id}</p>
                        <p className="mt-1 text-xs text-shell-soft">{task.assigned_to || "unassigned"}</p>
                      </div>
                      <StatusBadge status={task.status} />
                    </div>
                    <p className="mt-2 text-sm leading-6 text-shell-muted">{task.output || task.description || "No task detail persisted."}</p>
                  </div>
                ))}
                {(workflow.tasks?.length ?? 0) === 0 ? <EmptyState title="No tasks" body="This workflow has not persisted any tasks." /> : null}
              </div>
            ) : null}

            {selectedObject.id === "artifacts" ? (
              <div className="thin-scrollbar max-h-[28rem] space-y-3 overflow-auto pr-1">
                {(workflow.artifacts ?? []).map((artifact) => (
                  <div key={artifact.id} className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                    <div className="flex items-start justify-between gap-3">
                      <div>
                        <p className="text-sm font-semibold text-ink">{artifactLabel(artifact)}</p>
                        <p className="mt-1 text-xs text-shell-soft">{artifact.created_by || "unknown author"}</p>
                      </div>
                      <span className="rounded-full border border-shell-border/35 bg-shell-panel/90 px-2.5 py-1 text-xs font-semibold text-ink">
                        {artifact.kind || "artifact"}
                      </span>
                    </div>
                    <p className="mt-2 text-sm leading-6 text-shell-muted">
                      {contentPreview(artifact.content) || artifact.description || artifact.path || "No artifact content preview available."}
                    </p>
                  </div>
                ))}
                {(workflow.artifacts?.length ?? 0) === 0 ? <EmptyState title="No artifacts" body="This workflow has not persisted any artifacts." /> : null}
              </div>
            ) : null}

            {selectedObject.id === "summaries" ? (
              <div className="space-y-3">
                {Object.entries(workflow.summaries ?? {}).map(([key, value]) => (
                  <div key={key} className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                    <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">{key.replace(/_/g, " ")}</p>
                    <p className="mt-2 text-sm leading-6 text-shell-muted">{value}</p>
                  </div>
                ))}
                {Object.keys(workflow.summaries ?? {}).length === 0 ? <EmptyState title="No summaries" body="This workflow has no persona summaries yet." /> : null}
              </div>
            ) : null}

            {selectedObject.id === "finalization" ? (
              workflow.finalization ? (
                <div className="space-y-3">
                  {workflow.finalization.action ? (
                    <div className="rounded-3xl border border-lagoon/30 bg-lagoon/8 p-4">
                      <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Delivery action</p>
                      <p className="mt-2 text-sm font-medium text-ink">{workflow.finalization.action}</p>
                    </div>
                  ) : null}
                  <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                    <p className="text-sm font-semibold text-ink">Summary</p>
                    <p className="mt-2 text-sm leading-6 text-shell-muted">{workflow.finalization.summary || "No finalization summary persisted."}</p>
                  </div>
                  {(workflow.finalization.links?.length ?? 0) > 0 ? (
                    <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                      <p className="text-sm font-semibold text-ink">Delivery links</p>
                      <div className="mt-2 space-y-2">
                        {workflow.finalization.links?.map((link, index) => (
                          <a
                            key={`${link}-${index}`}
                            href={link}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="block truncate text-sm text-lagoon underline underline-offset-2 hover:text-lagoon/80"
                          >
                            {link}
                          </a>
                        ))}
                      </div>
                    </div>
                  ) : null}
                  {workflow.finalization.metadata && Object.keys(workflow.finalization.metadata).length > 0 ? (
                    <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                      <p className="text-sm font-semibold text-ink">Delivery metadata</p>
                      <div className="mt-2 grid gap-2">
                        {Object.entries(workflow.finalization.metadata).map(([key, value]) => (
                          <div key={key} className="flex items-start gap-2 text-sm">
                            <span className="shrink-0 font-medium text-shell-soft">{key}:</span>
                            <span className="min-w-0 break-all text-shell-muted">
                              {typeof value === "string" && (value.startsWith("http://") || value.startsWith("https://")) ? (
                                <a href={value} target="_blank" rel="noopener noreferrer" className="text-lagoon underline underline-offset-2 hover:text-lagoon/80">{value}</a>
                              ) : (
                                String(value)
                              )}
                            </span>
                          </div>
                        ))}
                      </div>
                    </div>
                  ) : null}
                  {(workflow.finalization.suggestions?.length ?? 0) > 0 ? (
                    <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                      <p className="text-sm font-semibold text-ink">Suggestions</p>
                      <div className="mt-2 space-y-2">
                        {workflow.finalization.suggestions?.map((suggestion, index) => (
                          <p key={`${suggestion}-${index}`} className="text-sm leading-6 text-shell-muted">{suggestion}</p>
                        ))}
                      </div>
                    </div>
                  ) : null}
                </div>
              ) : (
                <EmptyState title="No finalization data" body="The workflow has not reached a persisted finalization result." />
              )
            ) : null}

            {selectedObject.id === "blocking" ? (
              (workflow.blocking_issues?.length ?? 0) > 0 ? (
                <div className="space-y-3">
                  {workflow.blocking_issues?.map((issue, index) => (
                    <div key={`${issue}-${index}`} className="rounded-3xl border border-shell-warning/35 bg-shell-warning/12 p-4 text-sm text-shell-warning-text">
                      {issue}
                    </div>
                  ))}
                </div>
              ) : (
                <EmptyState title="No blocking issues" body="This run does not currently show any persisted blockers." />
              )
            ) : null}

            {selectedObject.id === "suggestions" ? (
              (workflow.all_suggestions?.length ?? 0) > 0 ? (
                <div className="space-y-3">
                  {workflow.all_suggestions?.map((suggestion, index) => (
                    <div key={`${suggestion}-${index}`} className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4 text-sm leading-6 text-shell-muted">
                      {suggestion}
                    </div>
                  ))}
                </div>
              ) : (
                <EmptyState title="No suggestions" body="No refinement suggestions were persisted for this run." />
              )
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
    </div>
  );
}

function WorkflowTaskBoard({ workflow }: { workflow: WorkflowState }) {
  const activeTaskId = workflow.execution?.active_task_id;
  const tasks = workflow.tasks ?? [];

  return (
    <div className="space-y-4">
      <div>
        <p className="eyebrow">Execution Plan</p>
        <h2 className="mt-2 font-display text-2xl font-semibold text-ink">Tasks and blockers</h2>
      </div>

      {workflow.error_message ? (
        <div className="rounded-3xl border border-shell-danger/30 bg-shell-danger/10 p-4 text-sm text-shell-danger-text">
          <p className="font-semibold">Workflow error</p>
          <p className="mt-2">{workflow.error_message}</p>
        </div>
      ) : null}

      {(workflow.blocking_issues?.length ?? 0) > 0 ? (
        <div className="rounded-3xl border border-shell-warning/35 bg-shell-warning/12 p-4 text-sm text-shell-warning-text">
          <p className="font-semibold">Blocking issues</p>
          <div className="mt-3 space-y-2">
            {workflow.blocking_issues?.map((issue, index) => (
              <p key={`${issue}-${index}`}>{issue}</p>
            ))}
          </div>
        </div>
      ) : null}

      {tasks.length === 0 ? (
        <EmptyState title="No tasks recorded" body="This workflow has not published a task graph yet." />
      ) : (
        <div className="thin-scrollbar max-h-[30rem] space-y-3 overflow-auto pr-1">
          {tasks.map((task) => {
            const active = activeTaskId === task.id;
            return (
              <div
                key={task.id}
                className={`rounded-3xl border p-4 transition ${
                  active
                    ? "border-lagoon bg-lagoon/12 shadow-[0_12px_32px_rgb(var(--color-lagoon)/0.12)]"
                    : "border-shell-border/40 bg-shell-panel/80"
                }`}
              >
                <div className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
                  <div className="min-w-0 space-y-2">
                    <p className="text-sm font-semibold text-ink [overflow-wrap:anywhere]">{task.title || task.id}</p>
                    <p className="text-sm leading-6 text-shell-muted [overflow-wrap:anywhere]">{task.description || task.output || "No task notes persisted."}</p>
                  </div>
                  <div className="flex shrink-0 items-center gap-3">
                    {active ? (
                      <span className="inline-flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">
                        <span className="relative flex h-2.5 w-2.5 shrink-0">
                          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-lagoon/60" />
                          <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-lagoon" />
                        </span>
                        Running now
                      </span>
                    ) : null}
                    <StatusBadge status={task.status} />
                  </div>
                </div>
                {(task.depends_on?.length ?? 0) > 0 ? (
                  <p className="mt-3 text-xs text-shell-soft [overflow-wrap:anywhere]">Depends on: {task.depends_on?.join(", ")}</p>
                ) : null}
              </div>
            );
          })}
        </div>
      )}

      {workflow.finalization?.summary ? (
        <div className="rounded-3xl border border-shell-border/40 bg-shell-subtle p-4">
          <p className="text-sm font-semibold text-ink">Finalization summary</p>
          <p className="mt-2 text-sm leading-6 text-shell-muted">{workflow.finalization.summary}</p>
        </div>
      ) : null}
    </div>
  );
}

function WorkflowDocument({ workflow }: { workflow: WorkflowState }) {
  return (
    <div className="space-y-4">
      <div>
        <p className="eyebrow">Workflow Document</p>
        <h2 className="mt-2 font-display text-2xl font-semibold text-ink">Raw persisted state</h2>
      </div>

      <p className="text-sm leading-6 text-shell-muted">
        Keep the state deck readable up top, then expand the full workflow payload only when you need to inspect the exact stored document.
      </p>

      <details className="rounded-3xl border border-shell-border/40 bg-shell-subtle open:shadow-[inset_0_1px_0_rgb(var(--color-shell-border)/0.12)]">
        <summary className="cursor-pointer list-none px-4 py-4 text-sm font-semibold text-ink">
          Show raw JSON document
        </summary>
        <div className="border-t border-shell-border/40 p-4">
          <pre className="thin-scrollbar max-h-[32rem] overflow-auto rounded-2xl bg-shell-code p-4 text-xs leading-6 text-shell-code-text">
            {prettyJson(workflow)}
          </pre>
        </div>
      </details>
    </div>
  );
}

export function WorkflowStudio() {
  const queryClient = useQueryClient();
  const workspace = useOrcaWorkspace();
  const createWorkflowLockRef = useRef(false);
  const explorerInitializedWorkflowIdRef = useRef<string | null>(null);
  const streamRefreshAtRef = useRef(0);
  const streamReconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search);
  const [page, setPage] = useState(0);
  const [selectedWorkflowId, setSelectedWorkflowId] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [streamConnected, setStreamConnected] = useState(false);
  const [streamEvents, setStreamEvents] = useState<EventRecord[]>([]);
  const [streamReconnectToken, setStreamReconnectToken] = useState(0);
  const [launchLocked, setLaunchLocked] = useState(false);
  const [workflowMessage, setWorkflowMessage] = useState<string | null>(null);
  const [explorerSelection, setExplorerSelection] = useState<WorkflowExplorerSelection>({
    kind: "object",
    id: "tasks",
  });
  const [formState, setFormState] = useState<
    CreateWorkflowRequest & { deliveryAction: string; deliveryConfig: string }
  >({
    request: "",
    title: "",
    mode: "software",
    provider: "",
    model: "",
    deliveryAction: "",
    deliveryConfig: "",
  });

  const workflowsQuery = useQuery({
    queryKey: ["workflows", workspace.tenantId, workspace.scopeId, page],
    queryFn: () => listWorkflows(workspace, 20, page * 20),
    refetchInterval: 2000,
    refetchIntervalInBackground: true,
  });

  const providersQuery = useQuery({
    queryKey: ["providers"],
    queryFn: () => listProviders(),
    staleTime: 60_000,
  });

  const modelsQuery = useQuery({
    queryKey: ["provider-models", formState.provider],
    queryFn: () => listProviderModels(formState.provider!),
    enabled: Boolean(formState.provider),
    staleTime: 60_000,
  });

  const filteredWorkflows = useMemo(() => {
    const needle = deferredSearch.trim().toLowerCase();
    if (!needle) {
      return workflowsQuery.data?.items ?? [];
    }

    return (workflowsQuery.data?.items ?? []).filter((workflow: WorkflowState) =>
      `${workflow.title ?? ""} ${workflow.request ?? ""} ${workflow.id}`.toLowerCase().includes(needle)
    );
  }, [deferredSearch, workflowsQuery.data?.items]);

  useEffect(() => {
    const candidateItems = filteredWorkflows.length > 0 ? filteredWorkflows : workflowsQuery.data?.items ?? [];
    if (candidateItems.length === 0) {
      if (selectedWorkflowId && !workflowsQuery.isFetching) {
        setSelectedWorkflowId("");
      }
      return;
    }

    if (!selectedWorkflowId) {
      setSelectedWorkflowId(candidateItems[0]?.id ?? "");
      setStreamEvents([]);
    }
  }, [filteredWorkflows, selectedWorkflowId, workflowsQuery.data?.items, workflowsQuery.isFetching]);

  const selectedWorkflowQuery = useQuery({
    queryKey: ["workflow", selectedWorkflowId, workspace.tenantId, workspace.scopeId],
    queryFn: () => getWorkflow(selectedWorkflowId, workspace),
    enabled: Boolean(selectedWorkflowId),
    refetchInterval: (query: { state: { data?: WorkflowState } }) => {
      if (!selectedWorkflowId) {
        return false;
      }

      return shouldRefreshWorkflowSnapshot(query.state.data) ? 2000 : false;
    },
    refetchIntervalInBackground: true,
  });

  useEffect(() => {
    const workflow = selectedWorkflowQuery.data;
    if (!workflow || workflow.id !== selectedWorkflowId) {
      return;
    }

    if (explorerInitializedWorkflowIdRef.current === workflow.id) {
      return;
    }

    explorerInitializedWorkflowIdRef.current = workflow.id;

    const activePersonaId = normalizePersonaId(workflow.execution?.current_persona);

    if (activePersonaId) {
      setExplorerSelection({ kind: "persona", id: activePersonaId });
      return;
    }

    setExplorerSelection({ kind: "object", id: "tasks" });
  }, [selectedWorkflowId, selectedWorkflowQuery.data]);

  const shouldAutoRefreshSelectedWorkflow = Boolean(selectedWorkflowId) && shouldRefreshWorkflowSnapshot(selectedWorkflowQuery.data);

  useEffect(() => {
    if (!selectedWorkflowId) {
      setStreaming(false);
      setStreamConnected(false);
      return;
    }

    const status = selectedWorkflowQuery.data?.status;
    if (!status) {
      setStreaming(false);
      setStreamConnected(false);
      return;
    }

    if (isWorkflowTerminal(status)) {
      setStreaming(false);
      setStreamConnected(false);
      return;
    }

    setStreaming(true);
  }, [selectedWorkflowId, selectedWorkflowQuery.data?.status]);

  const eventsQuery = useQuery({
    queryKey: ["workflow-events", selectedWorkflowId, workspace.tenantId, workspace.scopeId],
    queryFn: () => getWorkflowEvents(selectedWorkflowId, workspace),
    enabled: Boolean(selectedWorkflowId),
    refetchInterval: shouldAutoRefreshSelectedWorkflow ? 2000 : false,
    refetchIntervalInBackground: true,
  });

  useEffect(() => {
    if (!selectedWorkflowId || !(eventsQuery.data?.items?.length)) {
      return;
    }

    setStreamEvents((current) => mergeLiveFeedEvents(current, eventsQuery.data?.items ?? []));
  }, [eventsQuery.data?.items, selectedWorkflowId]);

  const refreshWorkflowQueries = async (workflowId?: string) => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["workflows"] }),
      queryClient.invalidateQueries({ queryKey: ["workflow", workflowId ?? selectedWorkflowId] }),
      queryClient.invalidateQueries({ queryKey: ["workflow-events", workflowId ?? selectedWorkflowId] }),
    ]);
  };

  useEffect(() => {
    if (!streaming || !selectedWorkflowId) {
      if (streamReconnectTimeoutRef.current) {
        clearTimeout(streamReconnectTimeoutRef.current);
        streamReconnectTimeoutRef.current = null;
      }
      setStreamConnected(false);
      return;
    }

    let source: EventSource | null = null;
    let cancelled = false;
    let reconnectScheduled = false;

    const refreshFromStream = () => {
      const now = Date.now();
      if (now - streamRefreshAtRef.current < 750) {
        return;
      }

      streamRefreshAtRef.current = now;
      void Promise.all([
        queryClient.invalidateQueries({ queryKey: ["workflows"] }),
        queryClient.invalidateQueries({ queryKey: ["workflow", selectedWorkflowId] }),
        queryClient.invalidateQueries({ queryKey: ["workflow-events", selectedWorkflowId] }),
      ]);
    };

    source = new EventSource(buildWorkflowStreamUrl(selectedWorkflowId, workspace));
    source.onopen = () => {
      setStreamConnected(true);
      setStreamEvents((current) => [
        {
          id: `stream-connected-${selectedWorkflowId}-${Date.now()}`,
          workflow_id: selectedWorkflowId,
          type: "stream.connected",
          payload: { workflow_id: selectedWorkflowId },
          occurred_at: new Date().toISOString(),
        },
        ...current,
      ].slice(0, 30));
    };

    source.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data) as EventRecord;
        setStreamEvents((current) => [payload, ...current].slice(0, 30));
        refreshFromStream();

        if (payload.type === "stream.closed") {
          source?.close();
          setStreamConnected(false);
          setStreaming(false);
        }
      } catch {
        setStreamEvents((current) => [
          {
            id: `${Date.now()}`,
            type: "stream.message",
            payload: event.data,
            occurred_at: new Date().toISOString(),
          },
          ...current,
        ].slice(0, 30));
        refreshFromStream();
      }
    };

    source.onerror = () => {
      source?.close();
      setStreamConnected(false);

      if (cancelled || reconnectScheduled) {
        return;
      }

      reconnectScheduled = true;
      setStreamEvents((current) => [
        {
          id: `stream-reconnecting-${selectedWorkflowId}-${Date.now()}`,
          workflow_id: selectedWorkflowId,
          type: "stream.reconnecting",
          payload: { workflow_id: selectedWorkflowId },
          occurred_at: new Date().toISOString(),
        },
        ...current,
      ].slice(0, 30));

      streamReconnectTimeoutRef.current = setTimeout(() => {
        if (cancelled) {
          return;
        }

        setStreamReconnectToken((current) => current + 1);
      }, 1500);
    };

    return () => {
      cancelled = true;
      if (streamReconnectTimeoutRef.current) {
        clearTimeout(streamReconnectTimeoutRef.current);
        streamReconnectTimeoutRef.current = null;
      }
      setStreamConnected(false);
      source?.close();
    };
  }, [queryClient, selectedWorkflowId, streaming, streamReconnectToken, workspace]);

  const createWorkflowMutation = useMutation({
    mutationFn: async () => {
      const request = formState.request.trim();
      if (!request) {
        throw new Error("Request is required");
      }

      let delivery: CreateWorkflowRequest["delivery"] | undefined;
      if (formState.deliveryAction) {
        delivery = {
          action: formState.deliveryAction.trim(),
          config: formState.deliveryConfig ? (JSON.parse(formState.deliveryConfig) as Record<string, unknown>) : undefined,
        };
      }

      return createWorkflow(
        {
          request,
          title: (formState.title ?? "").trim() || undefined,
          mode: formState.mode || undefined,
          provider: (formState.provider ?? "").trim() || undefined,
          model: (formState.model ?? "").trim() || undefined,
          delivery,
        },
        workspace
      );
    },
    onSuccess: async (workflow: WorkflowState) => {
      setWorkflowMessage(`Created workflow ${workflowLabel(workflow)}.`);
      setSelectedWorkflowId(workflow.id);
      setStreaming(false);
      setStreamConnected(false);
      setStreamEvents([]);
      explorerInitializedWorkflowIdRef.current = null;
      await refreshWorkflowQueries(workflow.id);
    },
    onError: (error: unknown) => {
      setWorkflowMessage(error instanceof Error ? error.message : "Failed to create workflow");
    },
  });

  const handleLaunchWorkflow = async () => {
    if (createWorkflowLockRef.current || createWorkflowMutation.isPending) {
      return;
    }

    if (!formState.request.trim()) {
      return;
    }

    createWorkflowLockRef.current = true;
    setLaunchLocked(true);
    setWorkflowMessage(null);

    try {
      await createWorkflowMutation.mutateAsync();
    } catch {
      // onError already surfaces the failure message in the UI.
    } finally {
      createWorkflowLockRef.current = false;
      setLaunchLocked(false);
    }
  };

  const cancelMutation = useMutation({
    mutationFn: () => cancelWorkflow(selectedWorkflowId, workspace),
    onSuccess: async () => {
      setWorkflowMessage("Workflow cancelled.");
      await refreshWorkflowQueries();
    },
    onError: (error: unknown) => setWorkflowMessage(error instanceof Error ? error.message : "Failed to cancel workflow"),
  });

  const resumeMutation = useMutation({
    mutationFn: () => resumeWorkflow(selectedWorkflowId, workspace),
    onSuccess: async () => {
      setWorkflowMessage("Workflow resumed.");
      await refreshWorkflowQueries();
    },
    onError: (error: unknown) => setWorkflowMessage(error instanceof Error ? error.message : "Failed to resume workflow"),
  });

  const selectedWorkflow = selectedWorkflowQuery.data;
  const workflowOptions = workflowsQuery.data?.items ?? [];
  const taskTotal = selectedWorkflow?.tasks?.length ?? 0;
  const taskDone = completedTaskCount(selectedWorkflow?.tasks);
  const canLaunchWorkflow = Boolean(formState.request.trim()) && !launchLocked && !createWorkflowMutation.isPending;
  const canCancelWorkflow = Boolean(selectedWorkflowId) && Boolean(selectedWorkflow) && !isWorkflowTerminal(selectedWorkflow?.status);
  const canResumeWorkflow =
    Boolean(selectedWorkflowId) &&
    (selectedWorkflow?.status === "paused" || selectedWorkflow?.status === "failed");

  return (
    <div className="space-y-6 pb-28 lg:pb-8">
      <Surface className="space-y-6">
        <SectionIntro
          eyebrow="Workflow Control"
          title="Launch, inspect, and stream go-orca runs"
          description="Tighten the selector, keep the active run above the fold, and turn the state surface into something operators can actually read under load."
          actions={<StatusBadge status={selectedWorkflow?.status} label={workflowStatusLabel(selectedWorkflow)} />}
        />

        <Surface className="space-y-4">
          <div className="flex flex-col gap-3 xl:flex-row xl:items-end xl:justify-between">
            <div>
              <p className="eyebrow">Create Workflow</p>
              <h2 className="mt-2 font-display text-2xl font-semibold text-ink">New request</h2>
            </div>
            <p className="max-w-2xl text-sm leading-6 text-shell-muted">
              Launch a new run against the current tenant and scope context without dropping below the fold.
            </p>
          </div>

          <div className="grid gap-4 xl:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]">
            <InputLabel label="Request" hint="Natural language task description.">
              <textarea
                rows={5}
                value={formState.request}
                onChange={(event) => setFormState((current) => ({ ...current, request: event.target.value }))}
                className={textFieldClassName()}
              />
            </InputLabel>

            <div className="grid gap-4 sm:grid-cols-2">
              <InputLabel label="Title">
                <input
                  value={formState.title}
                  onChange={(event) => setFormState((current) => ({ ...current, title: event.target.value }))}
                  className={textFieldClassName()}
                />
              </InputLabel>
              <InputLabel label="Mode">
                <select
                  value={formState.mode ?? ""}
                  onChange={(event) =>
                    setFormState((current) => ({
                      ...current,
                      mode: event.target.value as CreateWorkflowRequest["mode"],
                    }))
                  }
                  className={textFieldClassName()}
                >
                  {workflowModes.map((mode) => (
                    <option key={mode.value} value={mode.value}>
                      {mode.label}
                    </option>
                  ))}
                </select>
              </InputLabel>
              <InputLabel label="Provider" hint="Leave blank for server default.">
                <select
                  value={formState.provider}
                  onChange={(event) => setFormState((current) => ({ ...current, provider: event.target.value }))}
                  className={textFieldClassName()}
                >
                  <option value="">Server default</option>
                  {(providersQuery.data ?? []).map((p) => (
                    <option key={p.name} value={p.name}>{p.name}</option>
                  ))}
                </select>
              </InputLabel>
              <InputLabel label="Model" hint="Leave blank for provider default.">
                {formState.provider && (modelsQuery.data?.items?.length ?? 0) > 0 ? (
                  <select
                    value={formState.model}
                    onChange={(event) => setFormState((current) => ({ ...current, model: event.target.value }))}
                    className={textFieldClassName()}
                  >
                    <option value="">Provider default</option>
                    {(modelsQuery.data?.items ?? []).map((m) => (
                      <option key={m.id} value={m.id}>{m.name || m.id}</option>
                    ))}
                  </select>
                ) : (
                  <input
                    value={formState.model}
                    onChange={(event) => setFormState((current) => ({ ...current, model: event.target.value }))}
                    placeholder={formState.provider ? (modelsQuery.isLoading ? "Loading models…" : `Default for ${formState.provider}`) : "Auto-select"}
                    className={textFieldClassName()}
                  />
                )}
              </InputLabel>
              <InputLabel label="Delivery action">
                <select
                  value={formState.deliveryAction}
                  onChange={(event) => setFormState((current) => ({ ...current, deliveryAction: event.target.value, deliveryConfig: "" }))}
                  className={textFieldClassName()}
                >
                  {deliveryActions.map((action) => (
                    <option key={action.value} value={action.value}>
                      {action.label}
                    </option>
                  ))}
                </select>
              </InputLabel>
              {formState.deliveryAction === "github-pr" ? (
                <>
                  <InputLabel label="Repo" hint="owner/repo">
                    <input
                      value={(() => { try { return JSON.parse(formState.deliveryConfig || "{}").repo ?? ""; } catch { return ""; } })()}
                      onChange={(event) => setFormState((current) => {
                        const cfg = (() => { try { return JSON.parse(current.deliveryConfig || "{}"); } catch { return {}; } })();
                        return { ...current, deliveryConfig: JSON.stringify({ ...cfg, repo: event.target.value }) };
                      })}
                      placeholder="owner/repo"
                      className={textFieldClassName()}
                    />
                  </InputLabel>
                  <InputLabel label="Head branch">
                    <input
                      value={(() => { try { return JSON.parse(formState.deliveryConfig || "{}").head_branch ?? ""; } catch { return ""; } })()}
                      onChange={(event) => setFormState((current) => {
                        const cfg = (() => { try { return JSON.parse(current.deliveryConfig || "{}"); } catch { return {}; } })();
                        return { ...current, deliveryConfig: JSON.stringify({ ...cfg, head_branch: event.target.value }) };
                      })}
                      placeholder="feature/my-branch"
                      className={textFieldClassName()}
                    />
                  </InputLabel>
                  <InputLabel label="Base branch">
                    <input
                      value={(() => { try { return JSON.parse(formState.deliveryConfig || "{}").base_branch ?? ""; } catch { return ""; } })()}
                      onChange={(event) => setFormState((current) => {
                        const cfg = (() => { try { return JSON.parse(current.deliveryConfig || "{}"); } catch { return {}; } })();
                        return { ...current, deliveryConfig: JSON.stringify({ ...cfg, base_branch: event.target.value }) };
                      })}
                      placeholder="main"
                      className={textFieldClassName()}
                    />
                  </InputLabel>
                </>
              ) : formState.deliveryAction === "repo-commit-only" ? (
                <>
                  <InputLabel label="Repo" hint="owner/repo">
                    <input
                      value={(() => { try { return JSON.parse(formState.deliveryConfig || "{}").repo ?? ""; } catch { return ""; } })()}
                      onChange={(event) => setFormState((current) => {
                        const cfg = (() => { try { return JSON.parse(current.deliveryConfig || "{}"); } catch { return {}; } })();
                        return { ...current, deliveryConfig: JSON.stringify({ ...cfg, repo: event.target.value }) };
                      })}
                      placeholder="owner/repo"
                      className={textFieldClassName()}
                    />
                  </InputLabel>
                  <InputLabel label="Branch">
                    <input
                      value={(() => { try { return JSON.parse(formState.deliveryConfig || "{}").branch ?? ""; } catch { return ""; } })()}
                      onChange={(event) => setFormState((current) => {
                        const cfg = (() => { try { return JSON.parse(current.deliveryConfig || "{}"); } catch { return {}; } })();
                        return { ...current, deliveryConfig: JSON.stringify({ ...cfg, branch: event.target.value }) };
                      })}
                      placeholder="main"
                      className={textFieldClassName()}
                    />
                  </InputLabel>
                </>
              ) : formState.deliveryAction === "create-repo" ? (
                <>
                  <InputLabel label="Repo name">
                    <input
                      value={(() => { try { return JSON.parse(formState.deliveryConfig || "{}").name ?? ""; } catch { return ""; } })()}
                      onChange={(event) => setFormState((current) => {
                        const cfg = (() => { try { return JSON.parse(current.deliveryConfig || "{}"); } catch { return {}; } })();
                        return { ...current, deliveryConfig: JSON.stringify({ ...cfg, name: event.target.value }) };
                      })}
                      placeholder="my-new-repo"
                      className={textFieldClassName()}
                    />
                  </InputLabel>
                  <InputLabel label="Org / owner" hint="Leave blank for authenticated user.">
                    <input
                      value={(() => { try { return JSON.parse(formState.deliveryConfig || "{}").org ?? ""; } catch { return ""; } })()}
                      onChange={(event) => setFormState((current) => {
                        const cfg = (() => { try { return JSON.parse(current.deliveryConfig || "{}"); } catch { return {}; } })();
                        return { ...current, deliveryConfig: JSON.stringify({ ...cfg, org: event.target.value }) };
                      })}
                      placeholder="my-org"
                      className={textFieldClassName()}
                    />
                  </InputLabel>
                  <InputLabel label="Visibility">
                    <select
                      value={(() => { try { return JSON.parse(formState.deliveryConfig || "{}").private ? "private" : "public"; } catch { return "public"; } })()}
                      onChange={(event) => setFormState((current) => {
                        const cfg = (() => { try { return JSON.parse(current.deliveryConfig || "{}"); } catch { return {}; } })();
                        return { ...current, deliveryConfig: JSON.stringify({ ...cfg, private: event.target.value === "private" }) };
                      })}
                      className={textFieldClassName()}
                    >
                      <option value="public">Public</option>
                      <option value="private">Private</option>
                    </select>
                  </InputLabel>
                  <InputLabel label="Description">
                    <input
                      value={(() => { try { return JSON.parse(formState.deliveryConfig || "{}").description ?? ""; } catch { return ""; } })()}
                      onChange={(event) => setFormState((current) => {
                        const cfg = (() => { try { return JSON.parse(current.deliveryConfig || "{}"); } catch { return {}; } })();
                        return { ...current, deliveryConfig: JSON.stringify({ ...cfg, description: event.target.value }) };
                      })}
                      placeholder="Repository description"
                      className={textFieldClassName()}
                    />
                  </InputLabel>
                </>
              ) : formState.deliveryAction === "webhook-dispatch" ? (
                <InputLabel label="Webhook URL">
                  <input
                    value={(() => { try { return JSON.parse(formState.deliveryConfig || "{}").url ?? ""; } catch { return ""; } })()}
                    onChange={(event) => setFormState((current) => {
                      const cfg = (() => { try { return JSON.parse(current.deliveryConfig || "{}"); } catch { return {}; } })();
                      return { ...current, deliveryConfig: JSON.stringify({ ...cfg, url: event.target.value }) };
                    })}
                    placeholder="https://example.com/webhook"
                    className={textFieldClassName()}
                  />
                </InputLabel>
              ) : formState.deliveryAction && !["api-response", "markdown-export", "artifact-bundle", "blog-draft", "doc-draft", ""].includes(formState.deliveryAction) ? (
                <InputLabel label="Delivery config JSON" hint="Advanced: raw JSON config.">
                  <input
                    value={formState.deliveryConfig}
                    onChange={(event) => setFormState((current) => ({ ...current, deliveryConfig: event.target.value }))}
                    className={textFieldClassName()}
                  />
                </InputLabel>
              ) : null}
            </div>
          </div>

          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="space-y-1">
              <p className="text-sm text-shell-muted">The request will use the active routing headers shown in the shell.</p>
              {workflowMessage ? <p className="text-sm text-shell-muted">{workflowMessage}</p> : null}
            </div>
            <button
              type="button"
              onClick={() => {
                void handleLaunchWorkflow();
              }}
              disabled={!canLaunchWorkflow}
              className={primaryButtonClassName()}
            >
              <span className="inline-flex items-center gap-2">
                <WandSparkles className="h-4 w-4" />
                Launch workflow
              </span>
            </button>
          </div>
        </Surface>

        <div className="grid gap-4 xl:grid-cols-[340px_minmax(0,1fr)]">
          <div className="space-y-4">
            <Surface className="space-y-4 xl:sticky xl:top-4">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <p className="eyebrow">Workflow Selector</p>
                  <h2 className="mt-2 font-display text-2xl font-semibold text-ink">Latest runs</h2>
                </div>
                <div className="text-right text-xs text-shell-soft">{filteredWorkflows.length} visible</div>
              </div>

              <label className="relative block">
                <Search className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-shell-soft" />
                <input
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder="Filter by title, request, or id"
                  className={`${textFieldClassName()} pl-11 text-sm`}
                />
              </label>

              <div className="thin-scrollbar max-h-[30rem] space-y-3 overflow-auto pr-1">
                {filteredWorkflows.length === 0 ? (
                  <EmptyState title="No workflows found" body="Adjust the filter or launch a new workflow." />
                ) : (
                  filteredWorkflows.map((workflow: WorkflowState) => (
                    <button
                      key={workflow.id}
                      type="button"
                      onClick={() => {
                        setSelectedWorkflowId(workflow.id);
                        setStreaming(false);
                        setStreamConnected(false);
                        setStreamEvents([]);
                      }}
                      className={`w-full rounded-3xl border p-4 text-left transition ${
                        selectedWorkflowId === workflow.id
                          ? "border-lagoon bg-lagoon/12 shadow-[0_16px_40px_rgb(var(--color-lagoon)/0.12)]"
                          : "border-shell-border/40 bg-shell-panel/80 hover:border-lagoon"
                      }`}
                    >
                      <div className="flex flex-col gap-3">
                        <div className="flex items-center justify-between gap-3">
                          <p className="text-sm font-semibold text-ink">{workflowLabel(workflow)}</p>
                          <StatusBadge status={workflow.status} label={workflowStatusLabel(workflow)} />
                        </div>
                        <p className="text-sm leading-6 text-shell-muted">{summarizeText(workflow.request, 110)}</p>
                        <div className="flex flex-wrap items-center gap-3 text-xs text-shell-soft">
                          <span>{formatRelative(workflow.updated_at ?? workflow.created_at)}</span>
                          <span>{workflow.mode ?? "mode pending"}</span>
                          <span>ID {workflow.id.slice(0, 8)}</span>
                        </div>
                      </div>
                    </button>
                  ))
                )}
              </div>

              <div className="flex items-center justify-between pt-1">
                <button
                  type="button"
                  onClick={() => setPage((current) => Math.max(0, current - 1))}
                  disabled={page === 0}
                  className={secondaryButtonClassName()}
                >
                  Previous
                </button>
                <span className="text-sm text-shell-muted">Page {page + 1}</span>
                <button type="button" onClick={() => setPage((current) => current + 1)} className={secondaryButtonClassName()}>
                  Next
                </button>
              </div>
            </Surface>
          </div>

          <div className="space-y-4">
            <Surface className="space-y-5 overflow-hidden">
              <div className="flex flex-col gap-4 xl:flex-row xl:items-end xl:justify-between">
                <div>
                  <p className="eyebrow">Workflow State</p>
                  <h2 className="mt-2 font-display text-2xl font-semibold text-ink">Selected run</h2>
                </div>

                <div className="w-full xl:max-w-sm">
                  <InputLabel label="Workflow selector">
                    <select
                      value={selectedWorkflowId}
                      onChange={(event) => {
                        setSelectedWorkflowId(event.target.value);
                        setStreaming(false);
                        setStreamConnected(false);
                        setStreamEvents([]);
                      }}
                      className={textFieldClassName()}
                    >
                      {workflowOptions.map((workflow: WorkflowState) => (
                        <option key={workflow.id} value={workflow.id}>
                          {workflowLabel(workflow)}
                        </option>
                      ))}
                    </select>
                  </InputLabel>
                </div>
              </div>

              {selectedWorkflow ? (
                <>
                  <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                    <div className="min-w-0 rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                      <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Mode</p>
                      <p className="mt-2 text-sm font-medium text-ink [overflow-wrap:anywhere]">{selectedWorkflow.mode ?? "pending"}</p>
                    </div>
                    <div className="min-w-0 rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                      <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Current persona</p>
                      <p className="mt-2 text-sm font-medium text-ink [overflow-wrap:anywhere]">{workflowCurrentPersonaLabel(selectedWorkflow)}</p>
                    </div>
                    <div className="min-w-0 rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                      <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Task completion</p>
                      <p className="mt-2 text-sm font-medium text-ink [overflow-wrap:anywhere]">{taskDone} of {taskTotal}</p>
                    </div>
                    <div className="min-w-0 rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                      <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Artifacts</p>
                      <p className="mt-2 text-sm font-medium text-ink [overflow-wrap:anywhere]">{selectedWorkflow.artifacts?.length ?? 0} persisted</p>
                    </div>
                  </div>

                  <WorkflowVisualization
                    workflow={selectedWorkflow}
                    selectedPersonaId={explorerSelection.kind === "persona" ? explorerSelection.id : undefined}
                    onSelectPersona={(personaId) => setExplorerSelection({ kind: "persona", id: personaId })}
                    onSelectObject={(objectId) => setExplorerSelection({ kind: "object", id: objectId })}
                  />

                  <Surface className="space-y-4 bg-shell-panel/55">
                    <WorkflowExplorer
                      workflow={selectedWorkflow}
                      events={eventsQuery.data?.items ?? []}
                      selection={explorerSelection}
                      onSelect={setExplorerSelection}
                    />
                  </Surface>

                  <div className="flex flex-wrap items-center gap-3 text-sm text-shell-muted">
                    <StatusBadge status={selectedWorkflow.status} label={workflowStatusLabel(selectedWorkflow)} />
                    <span>ID {selectedWorkflow.id.slice(0, 8)}</span>
                    <span>Created {formatDate(selectedWorkflow.created_at)}</span>
                    <span>Updated {formatDate(selectedWorkflow.updated_at)}</span>
                    {selectedWorkflow.completed_at ? <span>Completed {formatDate(selectedWorkflow.completed_at)}</span> : null}
                    {streamConnected ? (
                      <span className="inline-flex items-center gap-2 rounded-full border border-lagoon/30 bg-lagoon/10 px-3 py-1 text-[0.68rem] font-semibold uppercase tracking-[0.16em] text-lagoon">
                        <span className="relative flex h-2.5 w-2.5 shrink-0">
                          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-lagoon/60" />
                          <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-lagoon" />
                        </span>
                        Live stream connected
                      </span>
                    ) : null}
                  </div>

                  <div className="flex flex-wrap gap-3">
                    <button
                      type="button"
                      onClick={() => setStreaming((current) => !current)}
                      className={primaryButtonClassName()}
                    >
                      <span className="inline-flex items-center gap-2">
                        <Radio className="h-4 w-4" />
                        {streaming ? "Pause live stream" : "Resume live stream"}
                      </span>
                    </button>
                    <button
                      type="button"
                      onClick={() => cancelMutation.mutate()}
                      disabled={!canCancelWorkflow || cancelMutation.isPending}
                      className={secondaryButtonClassName()}
                    >
                      <span className="inline-flex items-center gap-2">
                        <Square className="h-4 w-4" />
                        Cancel workflow
                      </span>
                    </button>
                    <button
                      type="button"
                      onClick={() => resumeMutation.mutate()}
                      disabled={!canResumeWorkflow || resumeMutation.isPending}
                      className={secondaryButtonClassName()}
                    >
                      <span className="inline-flex items-center gap-2">
                        <RotateCcw className="h-4 w-4" />
                        Resume workflow
                      </span>
                    </button>
                    <button type="button" onClick={() => refreshWorkflowQueries()} className={secondaryButtonClassName()}>
                      <span className="inline-flex items-center gap-2">
                        <Play className="h-4 w-4" />
                        Refresh
                      </span>
                    </button>
                  </div>
                </>
              ) : (
                <EmptyState title="No workflow selected" body="Pick a workflow from the selector to inspect the persisted state document." />
              )}
            </Surface>

            {selectedWorkflow ? (
              <div className="grid gap-4 xl:grid-cols-[0.85fr_1.15fr]">
                <Surface className="space-y-4">
                  <WorkflowTaskBoard workflow={selectedWorkflow} />
                </Surface>
                <Surface className="space-y-4">
                  <WorkflowDocument workflow={selectedWorkflow} />
                </Surface>
              </div>
            ) : null}

            <div className="grid gap-4 xl:grid-cols-2">
              <Surface className="space-y-4">
                <div>
                  <p className="eyebrow">Event Journal</p>
                  <h2 className="mt-2 font-display text-2xl font-semibold text-ink">Snapshot</h2>
                </div>
                <LiveEventList
                  events={eventsQuery.data?.items ?? []}
                  emptyTitle="No workflow events yet"
                  emptyBody="Pick a workflow with persisted events or launch a new run to start building its journal."
                />
              </Surface>

              <Surface className="space-y-4">
                <div>
                  <p className="eyebrow">SSE Stream</p>
                  <h2 className="mt-2 font-display text-2xl font-semibold text-ink">Live feed</h2>
                  <p className="mt-2 text-sm leading-6 text-shell-muted">
                    {streamConnected
                      ? "Connected to the selected workflow stream. Incoming persona and task events will land here immediately."
                      : selectedWorkflow && !isWorkflowTerminal(selectedWorkflow.status)
                        ? "The selected workflow auto-connects while it is active. Use the live stream toggle if you need to pause or reconnect."
                        : "Select a running workflow to attach a live event feed."}
                  </p>
                </div>
                <LiveEventList
                  events={streamEvents}
                  emptyTitle="No live events yet"
                  emptyBody="The live feed auto-attaches to active workflows. Pick a running workflow or reconnect the stream to watch events arrive in real time."
                />
              </Surface>
            </div>
          </div>
        </div>
      </Surface>
    </div>
  );
}