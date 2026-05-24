"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, Play, Save, TerminalSquare } from "lucide-react";
import { useOrcaWorkspace } from "./orca-workspace-provider";
import { createWorkflow, getWorkflowEvents, listWorkflows, updateWorkflowPlanning } from "../lib/orca/api";
import { formatRelative, prettyJson } from "../lib/orca/presentation";
import type { WorkflowState } from "../types/orca";
import {
  EmptyState,
  InputLabel,
  SectionIntro,
  Surface,
  primaryButtonClassName,
  secondaryButtonClassName,
  textFieldClassName,
} from "./ui";

function workflowName(workflow?: WorkflowState) {
  return workflow?.title || workflow?.request || workflow?.id || "workflow";
}

export function ApplicationBuilderStudio() {
  const queryClient = useQueryClient();
  const workspace = useOrcaWorkspace();
  const context = useMemo(
    () => ({ tenantId: workspace.tenantId || undefined, scopeId: workspace.scopeId || undefined }),
    [workspace.scopeId, workspace.tenantId]
  );

  const [request, setRequest] = useState("");
  const [planDraft, setPlanDraft] = useState("");
  const [activeWorkflowId, setActiveWorkflowId] = useState<string>("");

  const workflowsQuery = useQuery({
    queryKey: ["builder-workflows", context.tenantId, context.scopeId],
    queryFn: () => listWorkflows(context, 25, 0),
    refetchInterval: 6000,
  });

  const activeWorkflow = useMemo(
    () => workflowsQuery.data?.items.find((item) => item.id === activeWorkflowId),
    [activeWorkflowId, workflowsQuery.data?.items]
  );

  const eventsQuery = useQuery({
    queryKey: ["builder-events", activeWorkflowId, context.tenantId, context.scopeId],
    queryFn: () => getWorkflowEvents(activeWorkflowId, context),
    enabled: Boolean(activeWorkflowId),
    refetchInterval: 4000,
  });

  const createBuilderMutation = useMutation({
    mutationFn: () =>
      createWorkflow(
        {
          request: request.trim(),
          mode: "software",
          planning: {
            mode: "builder",
            prompt: request.trim(),
            plan: planDraft.trim() || undefined,
          },
        },
        context
      ),
    onSuccess: (workflow) => {
      setActiveWorkflowId(workflow.id);
      setPlanDraft(workflow.execution?.planning?.plan ?? "");
      queryClient.invalidateQueries({ queryKey: ["builder-workflows"] });
    },
  });

  const savePlanMutation = useMutation({
    mutationFn: () =>
      updateWorkflowPlanning(
        activeWorkflowId,
        {
          prompt: request.trim() || activeWorkflow?.execution?.planning?.prompt || undefined,
          plan: planDraft.trim(),
        },
        context
      ),
    onSuccess: (workflow) => {
      queryClient.setQueryData(
        ["builder-workflows", context.tenantId, context.scopeId],
        (current: { items: WorkflowState[] } | undefined) => {
          if (!current) return current;
          return {
            ...current,
            items: current.items.map((item) => (item.id === workflow.id ? workflow : item)),
          };
        }
      );
    },
  });

  const planning = activeWorkflow?.execution?.planning;
  const planningDecisions = planning?.decisions ?? [];
  const planningQuestions = planning?.questions ?? [];
  const events = eventsQuery.data?.items ?? [];

  return (
    <div className="space-y-4 pb-24 lg:pb-0">
      <Surface className="space-y-6">
        <SectionIntro
          eyebrow="Application Builder"
          title="Plan with Matriarch"
          description="Interactive planning workspace that asks Matriarch to derive an implementation-ready plan using available skills and MCP capabilities."
        />
        <div className="grid gap-4 xl:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
          <div className="space-y-4">
            <InputLabel label="Builder Request" hint="Describe the application or feature you want planned.">
              <textarea
                value={request}
                onChange={(event) => setRequest(event.target.value)}
                placeholder="Build a multi-tenant application builder with matriarch-led planning and auto-mode handoff."
                className={`${textFieldClassName()} min-h-[140px]`}
              />
            </InputLabel>
            <InputLabel label="Plan Draft" hint="This is persisted into workflow planning state for later implementation.">
              <textarea
                value={planDraft}
                onChange={(event) => setPlanDraft(event.target.value)}
                placeholder="Implementation plan draft..."
                className={`${textFieldClassName()} min-h-[240px]`}
              />
            </InputLabel>
            <div className="flex flex-wrap items-center gap-3">
              <button
                type="button"
                onClick={() => createBuilderMutation.mutate()}
                disabled={!request.trim() || createBuilderMutation.isPending}
                className={primaryButtonClassName()}
              >
                <span className="inline-flex items-center gap-2">
                  <Play className="h-4 w-4" />
                  {createBuilderMutation.isPending ? "Generating..." : "Generate Plan"}
                </span>
              </button>
              <button
                type="button"
                onClick={() => savePlanMutation.mutate()}
                disabled={!activeWorkflowId || savePlanMutation.isPending}
                className={secondaryButtonClassName()}
              >
                <span className="inline-flex items-center gap-2">
                  <Save className="h-4 w-4" />
                  Save Plan State
                </span>
              </button>
            </div>
          </div>

          <div className="space-y-4">
            <div className="rounded-3xl border border-shell-border/35 bg-shell-subtle/80 p-4">
              <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Active Builder Workflow</p>
              <p className="mt-2 text-sm font-medium text-ink">{workflowName(activeWorkflow)}</p>
              <p className="mt-1 text-xs text-shell-soft">
                {activeWorkflow ? `${activeWorkflow.status ?? "pending"} • ${formatRelative(activeWorkflow.updated_at)}` : "No workflow selected"}
              </p>
              <div className="mt-3">
                <select
                  className={`${textFieldClassName()} text-xs`}
                  value={activeWorkflowId}
                  onChange={(event) => setActiveWorkflowId(event.target.value)}
                >
                  <option value="">Select recent workflow</option>
                  {(workflowsQuery.data?.items ?? []).map((workflow) => (
                    <option key={workflow.id} value={workflow.id}>
                      {workflowName(workflow)}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            <div className="rounded-3xl border border-shell-border/35 bg-shell-subtle/80 p-4">
              <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Decisions</p>
              {planningDecisions.length > 0 ? (
                <ul className="mt-3 space-y-2 text-sm text-shell-muted">
                  {planningDecisions.map((item, idx) => (
                    <li key={`${idx}-${item}`} className="rounded-xl bg-shell-panel/70 px-3 py-2">
                      {item}
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="mt-2 text-sm text-shell-soft">Matriarch decisions will appear here.</p>
              )}
            </div>

            <div className="rounded-3xl border border-shell-border/35 bg-shell-subtle/80 p-4">
              <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Open Questions</p>
              {planningQuestions.length > 0 ? (
                <ul className="mt-3 space-y-2 text-sm text-shell-muted">
                  {planningQuestions.map((item, idx) => (
                    <li key={`${idx}-${item}`} className="rounded-xl bg-shell-panel/70 px-3 py-2">
                      {item}
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="mt-2 text-sm text-shell-soft">Critical unresolved questions will appear here.</p>
              )}
            </div>
          </div>
        </div>
      </Surface>

      <Surface className="space-y-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Interactive Prompt Terminal</p>
            <p className="mt-1 text-sm text-shell-muted">Event stream for builder workflows (persona starts, completions, and status updates).</p>
          </div>
          <TerminalSquare className="h-4 w-4 text-shell-soft" />
        </div>
        {events.length === 0 ? (
          <EmptyState title="No terminal events yet" body="Run a builder workflow to start streaming planning events." />
        ) : (
          <div className="thin-scrollbar max-h-[360px] space-y-2 overflow-y-auto rounded-2xl border border-shell-border/35 bg-shell-code/90 p-3 font-mono text-xs text-shell-code-text">
            {events.map((event) => (
              <div key={event.id} className="rounded-xl border border-shell-border/20 bg-black/20 p-2">
                <p className="text-[10px] uppercase tracking-[0.16em] text-shell-soft">
                  {(event.type ?? "event").replaceAll("_", " ")} • {event.persona ?? "system"}
                </p>
                <pre className="mt-2 whitespace-pre-wrap break-words">{prettyJson(event.payload ?? {})}</pre>
              </div>
            ))}
          </div>
        )}
      </Surface>

      <Surface className="space-y-3">
        <div className="flex items-center gap-2 text-lagoon">
          <Bot className="h-4 w-4" />
          <p className="text-xs font-semibold uppercase tracking-[0.16em]">Plan State Snapshot</p>
        </div>
        <pre className="thin-scrollbar max-h-[320px] overflow-auto rounded-2xl bg-shell-code p-4 text-xs text-shell-code-text">
          {prettyJson(activeWorkflow?.execution?.planning ?? null)}
        </pre>
      </Surface>
    </div>
  );
}
