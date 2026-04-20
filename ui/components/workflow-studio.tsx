"use client";

import { useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Bot,
  BrainCircuit,
  Boxes,
  CheckCircle2,
  Cpu,
  Download,
  FileCode2,
  FileText,
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

// ─── Artifact download helpers ────────────────────────────────────────────────

function artifactFileExtension(artifact: Artifact): string {
  if (artifact.name) {
    const dot = artifact.name.lastIndexOf(".");
    if (dot > 0) return artifact.name.slice(dot);
  }
  switch (artifact.kind) {
    case "code": return ".txt";
    case "markdown":
    case "document":
    case "blog_post":
    case "report":
    case "diagram": return ".md";
    case "config": return ".yaml";
    default: return ".txt";
  }
}

function triggerDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

function downloadArtifact(artifact: Artifact) {
  const content = artifact.content ?? "";
  const ext = artifactFileExtension(artifact);
  const baseName = artifact.name ?? artifact.path ?? `artifact-${artifact.id}`;
  const filename = baseName.includes(".") ? baseName : `${baseName}${ext}`;
  triggerDownload(new Blob([content], { type: "text/plain;charset=utf-8" }), filename);
}

function downloadArtifactBundle(artifacts: Artifact[], workflowId: string) {
  const sep = "=".repeat(72);
  const parts = artifacts.map((a) =>
    `${sep}\n${artifactLabel(a)}  [${a.kind ?? "artifact"}]\n${sep}\n${a.content ?? "(no content)"}`
  );
  const bundle = parts.join("\n\n") + "\n";
  triggerDownload(
    new Blob([bundle], { type: "text/plain;charset=utf-8" }),
    `workflow-${workflowId.slice(0, 8)}-bundle.txt`
  );
}

function isCodeKind(kind?: string) {
  return kind === "code" || kind === "config";
}

function isDocKind(kind?: string) {
  return (
    kind === "markdown" ||
    kind === "document" ||
    kind === "blog_post" ||
    kind === "report" ||
    kind === "diagram"
  );
}

// ─── ArtifactViewer ───────────────────────────────────────────────────────────

function ArtifactViewer({
  artifact,
  isLive = false,
  autoFollow = false,
  showDownload = true,
}: {
  artifact: Artifact;
  isLive?: boolean;
  autoFollow?: boolean;
  showDownload?: boolean;
}) {
  const content = artifact.content ?? "";
  const lines = content.split("\n");
  const label = artifactLabel(artifact);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [userScrolledUp, setUserScrolledUp] = useState(false);

  // Auto-scroll to bottom when live + following, unless user scrolled up.
  useEffect(() => {
    if (!isLive || !autoFollow || userScrolledUp) return;
    const el = scrollRef.current;
    if (el) {
      el.scrollTop = el.scrollHeight;
    }
  });

  function handleScroll() {
    const el = scrollRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    setUserScrolledUp(!atBottom);
  }

  if (isCodeKind(artifact.kind)) {
    return (
      <div className="overflow-hidden rounded-2xl border border-[#30363d] bg-[#0d1117] font-mono text-sm">
        {/* macOS-style title bar */}
        <div className="flex items-center gap-3 border-b border-[#21262d] bg-[#161b22] px-4 py-2.5">
          <div className="flex gap-1.5 shrink-0">
            <span className="h-3 w-3 rounded-full bg-[#ff5f57]" />
            <span className="h-3 w-3 rounded-full bg-[#febc2e]" />
            <span className="h-3 w-3 rounded-full bg-[#28c840]" />
          </div>
          <FileCode2 className="h-3.5 w-3.5 shrink-0 text-[#7d8590]" />
          <span className="min-w-0 truncate text-xs text-[#c9d1d9]">{label}</span>
          <div className="ml-auto flex shrink-0 items-center gap-2">
            {isLive ? (
              <span className="inline-flex items-center gap-1.5 rounded-full bg-lagoon/15 px-2 py-0.5 text-[0.6rem] font-semibold uppercase tracking-widest text-lagoon">
                <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-lagoon" />
                writing
              </span>
            ) : (
              <span className="rounded-full bg-[#1f2937] px-2 py-0.5 text-[0.62rem] font-medium text-[#7d8590]">
                {artifact.kind}
              </span>
            )}
            {showDownload ? (
              <button
                type="button"
                onClick={() => downloadArtifact(artifact)}
                className="inline-flex items-center gap-1.5 rounded-full border border-[#30363d] bg-[#21262d] px-2.5 py-1 text-[0.68rem] font-medium text-[#7d8590] transition hover:border-lagoon/50 hover:text-lagoon"
              >
                <Download className="h-3 w-3" />
                Download
              </button>
            ) : null}
          </div>
        </div>

        {/* Code body with line numbers */}
        <div
          ref={scrollRef}
          onScroll={handleScroll}
          className="thin-scrollbar max-h-[60vh] overflow-auto"
        >
          <table className="w-full border-collapse text-xs leading-6">
            <tbody>
              {lines.map((line, index) => (
                <tr key={index} className="group hover:bg-[#161b22]">
                  <td
                    className="select-none border-r border-[#21262d] px-4 text-right text-[#484f58] group-hover:text-[#6e7681]"
                    style={{ minWidth: "3.25rem" }}
                  >
                    {index + 1}
                  </td>
                  <td className="whitespace-pre px-4 text-[#e6edf3]">{line || "\u00a0"}</td>
                </tr>
              ))}
              {isLive ? (
                <tr>
                  <td
                    className="select-none border-r border-[#21262d] px-4 text-right text-[#484f58]"
                    style={{ minWidth: "3.25rem" }}
                  >
                    {lines.length + 1}
                  </td>
                  <td className="px-4">
                    <span className="inline-block h-[0.9em] w-[2px] translate-y-[1px] animate-pulse bg-lagoon" />
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>

        {/* Follow / line count footer */}
        <div className="flex items-center justify-between border-t border-[#21262d] bg-[#161b22] px-4 py-2">
          <span className="text-[0.65rem] text-[#484f58]">{lines.length} lines</span>
          {isLive ? (
            <button
              type="button"
              onClick={() => { setUserScrolledUp(false); const el = scrollRef.current; if (el) el.scrollTop = el.scrollHeight; }}
              className={`text-[0.65rem] font-medium transition ${userScrolledUp ? "text-lagoon hover:text-lagoon/80" : "text-[#484f58]"}`}
            >
              {userScrolledUp ? "↓ Follow output" : "Following…"}
            </button>
          ) : null}
        </div>
      </div>
    );
  }

  if (isDocKind(artifact.kind)) {
    // Segment content into paragraphs for document-style rendering.
    const segments = content.split(/\n{2,}/);
    return (
      <div className="overflow-hidden rounded-2xl border border-shell-border/40 bg-shell-panel/90">
        {/* Document header */}
        <div className="flex items-center gap-3 border-b border-shell-border/25 bg-shell-subtle px-5 py-3">
          <FileText className="h-3.5 w-3.5 shrink-0 text-shell-soft" />
          <span className="min-w-0 truncate text-xs font-medium text-ink">{label}</span>
          <div className="ml-auto flex shrink-0 items-center gap-2">
            {isLive ? (
              <span className="inline-flex items-center gap-1.5 rounded-full bg-lagoon/10 px-2 py-0.5 text-[0.6rem] font-semibold uppercase tracking-widest text-lagoon">
                <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-lagoon" />
                writing
              </span>
            ) : (
              <span className="rounded-full border border-shell-border/35 bg-shell-panel/90 px-2 py-0.5 text-[0.62rem] font-medium text-shell-soft">
                {artifact.kind}
              </span>
            )}
            {showDownload ? (
              <button
                type="button"
                onClick={() => downloadArtifact(artifact)}
                className="inline-flex items-center gap-1.5 rounded-full border border-shell-border/40 bg-shell-panel/80 px-2.5 py-1 text-[0.68rem] font-medium text-shell-soft transition hover:border-lagoon/50 hover:text-lagoon"
              >
                <Download className="h-3 w-3" />
                Download
              </button>
            ) : null}
          </div>
        </div>

        {/* Document body */}
        <div
          ref={scrollRef}
          onScroll={handleScroll}
          className="thin-scrollbar max-h-[60vh] overflow-auto px-8 py-6"
        >
          <div className="space-y-4 text-sm leading-7 text-ink">
            {segments.map((segment, index) => {
              const trimmed = segment.trim();
              if (!trimmed) return null;
              if (trimmed.startsWith("### ")) {
                return (
                  <h3 key={index} className="mt-2 text-base font-semibold text-ink">
                    {trimmed.slice(4)}
                  </h3>
                );
              }
              if (trimmed.startsWith("## ")) {
                return (
                  <h2 key={index} className="mt-4 font-display text-lg font-semibold text-ink">
                    {trimmed.slice(3)}
                  </h2>
                );
              }
              if (trimmed.startsWith("# ")) {
                return (
                  <h1 key={index} className="mt-4 font-display text-xl font-semibold text-ink">
                    {trimmed.slice(2)}
                  </h1>
                );
              }
              if (trimmed.startsWith("```")) {
                const code = trimmed.replace(/^```[^\n]*\n?/, "").replace(/\n?```$/, "");
                return (
                  <pre key={index} className="thin-scrollbar overflow-x-auto rounded-xl bg-shell-code p-4 text-xs leading-6 text-shell-code-text">
                    {code}
                  </pre>
                );
              }
              if (/^[-*] /.test(trimmed)) {
                return (
                  <ul key={index} className="ml-5 list-disc space-y-1 text-shell-muted">
                    {trimmed.split("\n").filter(Boolean).map((item, i) => (
                      <li key={i}>{item.replace(/^[-*]\s+/, "")}</li>
                    ))}
                  </ul>
                );
              }
              if (/^\d+\. /.test(trimmed)) {
                return (
                  <ol key={index} className="ml-5 list-decimal space-y-1 text-shell-muted">
                    {trimmed.split("\n").filter(Boolean).map((item, i) => (
                      <li key={i}>{item.replace(/^\d+\.\s+/, "")}</li>
                    ))}
                  </ol>
                );
              }
              return (
                <p key={index} className="text-shell-muted [overflow-wrap:anywhere]">
                  {trimmed}
                </p>
              );
            })}
            {isLive ? (
              <span className="inline-block h-[0.9em] w-[2px] translate-y-[1px] animate-pulse bg-lagoon" />
            ) : null}
          </div>
        </div>

        {/* Word count / follow footer */}
        <div className="flex items-center justify-between border-t border-shell-border/25 bg-shell-subtle px-5 py-2">
          <span className="text-[0.65rem] text-shell-soft">{content.split(/\s+/).filter(Boolean).length} words</span>
          {isLive ? (
            <button
              type="button"
              onClick={() => { setUserScrolledUp(false); const el = scrollRef.current; if (el) el.scrollTop = el.scrollHeight; }}
              className={`text-[0.65rem] font-medium transition ${userScrolledUp ? "text-lagoon hover:text-lagoon/80" : "text-shell-soft"}`}
            >
              {userScrolledUp ? "↓ Follow writing" : "Following…"}
            </button>
          ) : null}
        </div>
      </div>
    );
  }

  // Fallback: raw scrollable pre block with download
  return (
    <div className="overflow-hidden rounded-2xl border border-shell-border/40 bg-shell-panel/90">
      <div className="flex items-center gap-3 border-b border-shell-border/25 bg-shell-subtle px-5 py-3">
        <FileText className="h-3.5 w-3.5 shrink-0 text-shell-soft" />
        <span className="min-w-0 truncate text-xs font-medium text-ink">{label}</span>
        <div className="ml-auto flex shrink-0 items-center gap-2">
          <span className="rounded-full border border-shell-border/35 bg-shell-panel/90 px-2 py-0.5 text-[0.62rem] font-medium text-shell-soft">
            {artifact.kind ?? "artifact"}
          </span>
          {showDownload ? (
            <button
              type="button"
              onClick={() => downloadArtifact(artifact)}
              className="inline-flex items-center gap-1.5 rounded-full border border-shell-border/40 bg-shell-panel/80 px-2.5 py-1 text-[0.68rem] font-medium text-shell-soft transition hover:border-lagoon/50 hover:text-lagoon"
            >
              <Download className="h-3 w-3" />
              Download
            </button>
          ) : null}
        </div>
      </div>
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="thin-scrollbar max-h-[60vh] overflow-auto"
      >
        <pre className="p-5 text-xs leading-6 text-shell-code-text">
          {content || "(no content)"}
        </pre>
      </div>
    </div>
  );
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
                <div className="thin-scrollbar max-h-[32rem] space-y-3 overflow-auto pr-1">
                  {selectedPersona.artifacts.map((artifact) => (
                    <ArtifactViewer key={artifact.id} artifact={artifact} />
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
              <div className="space-y-4">
                {(workflow.artifacts?.length ?? 0) > 1 ? (
                  <div className="flex justify-end">
                    <button
                      type="button"
                      onClick={() => downloadArtifactBundle(workflow.artifacts ?? [], workflow.id)}
                      className="inline-flex items-center gap-2 rounded-full border border-shell-border/40 bg-shell-panel/80 px-4 py-2 text-xs font-medium text-shell-soft transition hover:border-lagoon/50 hover:text-lagoon"
                    >
                      <Download className="h-3.5 w-3.5" />
                      Download all as bundle
                    </button>
                  </div>
                ) : null}
                <div className="space-y-4">
                  {(workflow.artifacts ?? []).map((artifact) => (
                    <ArtifactViewer key={artifact.id} artifact={artifact} />
                  ))}
                </div>
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
                      <div className="mt-2 flex items-center justify-between gap-3">
                        <p className="text-sm font-medium text-ink">{workflow.finalization.action}</p>
                        {workflow.finalization.action === "artifact-bundle" && (workflow.artifacts?.length ?? 0) > 0 ? (
                          <button
                            type="button"
                            onClick={() => downloadArtifactBundle(workflow.artifacts ?? [], workflow.id)}
                            className="inline-flex shrink-0 items-center gap-1.5 rounded-full border border-lagoon/40 bg-lagoon/10 px-3 py-1.5 text-xs font-medium text-lagoon transition hover:bg-lagoon/20"
                          >
                            <Download className="h-3.5 w-3.5" />
                            Download bundle
                          </button>
                        ) : null}
                      </div>
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
  const router = useRouter();
  const searchParams = useSearchParams();
  const createWorkflowLockRef = useRef(false);
  const explorerInitializedWorkflowIdRef = useRef<string | null>(null);
  const streamRefreshAtRef = useRef(0);
  const streamReconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search);
  const [page, setPage] = useState(0);
  const [selectedWorkflowId, setSelectedWorkflowId] = useState(() => searchParams.get("id") ?? "");
  const [liveArtifactId, setLiveArtifactId] = useState("");
  const [studioTab, setStudioTab] = useState<"status" | "explorer" | "artifacts" | "events" | "document">("status");
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

  // Auto-select the most recently added artifact for the live viewer.
  // When a new artifact arrives during an active workflow, also jump to the Artifacts tab.
  useEffect(() => {
    const artifacts = selectedWorkflowQuery.data?.artifacts;
    if (!artifacts || artifacts.length === 0) {
      setLiveArtifactId("");
      return;
    }
    const newestId = artifacts[artifacts.length - 1]?.id ?? "";
    setLiveArtifactId((current) => {
      if (current && artifacts.some((a) => a.id === current)) return current;
      // A new artifact appeared — switch to it and open the Artifacts tab.
      if (newestId && newestId !== current) {
        const status = selectedWorkflowQuery.data?.status;
        if (status && !isWorkflowTerminal(status)) {
          setStudioTab("artifacts");
        }
      }
      return newestId;
    });
  }, [selectedWorkflowQuery.data?.artifacts, selectedWorkflowQuery.data?.status]);

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
      setLiveArtifactId("");
      setStudioTab("status");
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
  const taskTotal = selectedWorkflow?.tasks?.length ?? 0;
  const taskDone = completedTaskCount(selectedWorkflow?.tasks);
  const canLaunchWorkflow = Boolean(formState.request.trim()) && !launchLocked && !createWorkflowMutation.isPending;
  const canCancelWorkflow = Boolean(selectedWorkflowId) && Boolean(selectedWorkflow) && !isWorkflowTerminal(selectedWorkflow?.status);
  const canResumeWorkflow =
    Boolean(selectedWorkflowId) &&
    (selectedWorkflow?.status === "paused" || selectedWorkflow?.status === "failed");

  const artifactCount = selectedWorkflow?.artifacts?.length ?? 0;
  const hasBlockingIssues = (selectedWorkflow?.blocking_issues?.length ?? 0) > 0;

  const studioTabs = [
    { id: "status" as const, label: "Status", badge: hasBlockingIssues ? "!" : undefined },
    { id: "explorer" as const, label: "Explorer", badge: undefined as string | undefined },
    { id: "artifacts" as const, label: "Artifacts", badge: artifactCount > 0 ? String(artifactCount) : undefined },
    { id: "events" as const, label: "Events", badge: streamConnected ? "●" : undefined },
    { id: "document" as const, label: "Document", badge: undefined as string | undefined },
  ] as const;

  function selectWorkflow(id: string) {
    setSelectedWorkflowId(id);
    setStreaming(false);
    setStreamConnected(false);
    setStreamEvents([]);
    setLiveArtifactId("");
    setStudioTab("status");
    router.replace(`/workflows?id=${id}`, { scroll: false });
  }

  return (
    <div className="space-y-6 pb-28 lg:pb-8">
      <Surface className="space-y-6">
        <SectionIntro
          eyebrow="Workflow Control"
          title="Launch, inspect, and stream go-orca runs"
          description="Submit a request, pick a run from the selector, then use the tabs to track status, explore outputs, view live artifacts, and stream events."
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

        {/* ── Selector + tabbed detail ─────────────────────────────────────── */}
        <div className="grid gap-4 xl:grid-cols-[300px_minmax(0,1fr)]">

          {/* Left: workflow selector */}
          <div className="space-y-4">
            <Surface className="space-y-4 xl:sticky xl:top-4">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <p className="eyebrow">Runs</p>
                  <h2 className="mt-1 font-display text-xl font-semibold text-ink">Workflow selector</h2>
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
                      onClick={() => selectWorkflow(workflow.id)}
                      className={`w-full rounded-3xl border p-4 text-left transition ${
                        selectedWorkflowId === workflow.id
                          ? "border-lagoon bg-lagoon/12 shadow-[0_16px_40px_rgb(var(--color-lagoon)/0.12)]"
                          : "border-shell-border/40 bg-shell-panel/80 hover:border-lagoon"
                      }`}
                    >
                      <div className="flex flex-col gap-2">
                        <div className="flex items-start justify-between gap-2">
                          <p className="text-sm font-semibold text-ink leading-snug [overflow-wrap:anywhere]">{workflowLabel(workflow)}</p>
                          <StatusBadge status={workflow.status} label={workflowStatusLabel(workflow)} />
                        </div>
                        <div className="flex flex-wrap items-center gap-2 text-xs text-shell-soft">
                          <span>{formatRelative(workflow.updated_at ?? workflow.created_at)}</span>
                          <span>{workflow.mode ?? "mode pending"}</span>
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

          {/* Right: tabbed detail panel */}
          <div className="min-w-0 space-y-0">

            {/* Tab bar */}
            <div className="flex items-center gap-1 overflow-x-auto rounded-t-[1.75rem] border border-b-0 border-shell-border/40 bg-shell-subtle px-4 pt-4">
              {studioTabs.map((tab) => {
                const active = studioTab === tab.id;
                return (
                  <button
                    key={tab.id}
                    type="button"
                    onClick={() => setStudioTab(tab.id)}
                    className={`relative flex shrink-0 items-center gap-2 rounded-t-xl px-4 py-2.5 text-sm font-medium transition ${
                      active
                        ? "bg-shell-panel text-ink shadow-[0_-1px_0_0_rgb(var(--color-shell-panel))]"
                        : "text-shell-muted hover:text-ink"
                    }`}
                  >
                    {tab.label}
                    {tab.badge ? (
                      <span
                        className={`inline-flex min-w-[1.25rem] items-center justify-center rounded-full px-1.5 py-0.5 text-[0.6rem] font-bold leading-none ${
                          tab.badge === "!" ? "bg-amber-400/25 text-amber-700 dark:text-amber-300"
                          : tab.badge === "●" ? "bg-lagoon/20 text-lagoon"
                          : "bg-shell-border/40 text-shell-muted"
                        }`}
                      >
                        {tab.badge}
                      </span>
                    ) : null}
                    {active ? (
                      <span className="absolute bottom-0 left-4 right-4 h-px bg-shell-panel" />
                    ) : null}
                  </button>
                );
              })}
            </div>

            {/* Tab panels */}
            <div className="rounded-b-[1.75rem] rounded-tr-[1.75rem] border border-shell-border/40 bg-shell-panel p-5">

              {/* ── Status tab ──────────────────────────────────────────────── */}
              {studioTab === "status" ? (
                <div className="space-y-5">
                  {selectedWorkflow ? (
                    <>
                      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                        <div className="min-w-0">
                          <p className="text-lg font-semibold text-ink [overflow-wrap:anywhere]">{workflowLabel(selectedWorkflow)}</p>
                          <p className="mt-1 text-sm leading-6 text-shell-muted">{summarizeText(selectedWorkflow.request, 160)}</p>
                        </div>
                        <StatusBadge status={selectedWorkflow.status} label={workflowStatusLabel(selectedWorkflow)} />
                      </div>

                      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                        <div className="min-w-0 rounded-3xl border border-shell-border/40 bg-shell-subtle p-4">
                          <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Mode</p>
                          <p className="mt-2 text-sm font-medium text-ink [overflow-wrap:anywhere]">{selectedWorkflow.mode ?? "pending"}</p>
                        </div>
                        <div className="min-w-0 rounded-3xl border border-shell-border/40 bg-shell-subtle p-4">
                          <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Current persona</p>
                          <p className="mt-2 text-sm font-medium text-ink [overflow-wrap:anywhere]">{workflowCurrentPersonaLabel(selectedWorkflow)}</p>
                        </div>
                        <div className="min-w-0 rounded-3xl border border-shell-border/40 bg-shell-subtle p-4">
                          <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Task completion</p>
                          <p className="mt-2 text-sm font-medium text-ink [overflow-wrap:anywhere]">{taskDone} of {taskTotal}</p>
                        </div>
                        <button
                          type="button"
                          onClick={() => setStudioTab("artifacts")}
                          className="min-w-0 rounded-3xl border border-shell-border/40 bg-shell-subtle p-4 text-left transition hover:border-lagoon/40"
                        >
                          <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Artifacts</p>
                          <p className="mt-2 text-sm font-medium text-ink">{artifactCount} persisted</p>
                        </button>
                      </div>

                      <WorkflowVisualization
                        workflow={selectedWorkflow}
                        selectedPersonaId={explorerSelection.kind === "persona" ? explorerSelection.id : undefined}
                        onSelectPersona={(personaId) => {
                          setExplorerSelection({ kind: "persona", id: personaId });
                          setStudioTab("explorer");
                        }}
                        onSelectObject={(objectId) => {
                          setExplorerSelection({ kind: "object", id: objectId });
                          setStudioTab("explorer");
                        }}
                      />

                      <Surface className="space-y-4 bg-shell-subtle">
                        <WorkflowTaskBoard workflow={selectedWorkflow} />
                      </Surface>

                      <div className="flex flex-wrap items-center gap-3 text-sm text-shell-muted">
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
                            Cancel
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
                            Resume
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
                    <EmptyState title="No workflow selected" body="Pick a workflow from the selector on the left." />
                  )}
                </div>
              ) : null}

              {/* ── Explorer tab ─────────────────────────────────────────────── */}
              {studioTab === "explorer" ? (
                <div className="space-y-4">
                  {selectedWorkflow ? (
                    <WorkflowExplorer
                      workflow={selectedWorkflow}
                      events={eventsQuery.data?.items ?? []}
                      selection={explorerSelection}
                      onSelect={setExplorerSelection}
                    />
                  ) : (
                    <EmptyState title="No workflow selected" body="Pick a workflow from the selector on the left." />
                  )}
                </div>
              ) : null}

              {/* ── Artifacts tab ────────────────────────────────────────────── */}
              {studioTab === "artifacts" ? (
                <div className="space-y-4">
                  {artifactCount > 0 ? (() => {
                    const artifacts = selectedWorkflow?.artifacts ?? [];
                    const isActive = selectedWorkflow != null && !isWorkflowTerminal(selectedWorkflow.status);
                    const activeArtifact =
                      artifacts.find((a) => a.id === liveArtifactId) ?? artifacts[artifacts.length - 1];

                    return (
                      <>
                        {/* Artifact pill strip */}
                        <div className="flex flex-wrap items-center gap-2">
                          {artifacts.map((artifact) => {
                            const selected = artifact.id === activeArtifact?.id;
                            const isNewest = artifact.id === artifacts[artifacts.length - 1]?.id;
                            return (
                              <button
                                key={artifact.id}
                                type="button"
                                onClick={() => setLiveArtifactId(artifact.id)}
                                className={`inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs font-medium transition ${
                                  selected
                                    ? "border-lagoon bg-lagoon/10 text-lagoon"
                                    : "border-shell-border/40 bg-shell-subtle text-shell-muted hover:border-lagoon/40 hover:text-ink"
                                }`}
                              >
                                {isCodeKind(artifact.kind) ? (
                                  <FileCode2 className="h-3 w-3 shrink-0" />
                                ) : (
                                  <FileText className="h-3 w-3 shrink-0" />
                                )}
                                <span className="max-w-[12rem] truncate">{artifactLabel(artifact)}</span>
                                <span className={`rounded-full px-1.5 py-0.5 text-[0.58rem] font-semibold uppercase tracking-wider ${selected ? "bg-lagoon/20 text-lagoon" : "bg-shell-border/30 text-shell-soft"}`}>
                                  {artifact.kind ?? "artifact"}
                                </span>
                                {isActive && isNewest ? (
                                  <span className="h-1.5 w-1.5 shrink-0 animate-pulse rounded-full bg-lagoon" />
                                ) : null}
                              </button>
                            );
                          })}

                          {artifactCount > 1 ? (
                            <button
                              type="button"
                              onClick={() => downloadArtifactBundle(artifacts, selectedWorkflow?.id ?? "")}
                              className="ml-auto inline-flex items-center gap-1.5 rounded-full border border-shell-border/40 bg-shell-subtle px-3 py-1.5 text-xs font-medium text-shell-soft transition hover:border-lagoon/50 hover:text-lagoon"
                            >
                              <Download className="h-3 w-3" />
                              Download all
                            </button>
                          ) : null}
                        </div>

                        {/* Live viewer — full width, auto-follows writing */}
                        {activeArtifact ? (
                          <ArtifactViewer
                            artifact={activeArtifact}
                            isLive={isActive}
                            autoFollow={isActive}
                          />
                        ) : null}
                      </>
                    );
                  })() : (
                    <EmptyState
                      title="No artifacts yet"
                      body={
                        selectedWorkflow
                          ? "Artifacts appear here as the Implementer generates them. Check the Status tab for progress."
                          : "Pick a workflow from the selector on the left."
                      }
                    />
                  )}
                </div>
              ) : null}

              {/* ── Events tab ───────────────────────────────────────────────── */}
              {studioTab === "events" ? (
                <div className="space-y-6">
                  <div className="grid gap-5 xl:grid-cols-2">
                    <div className="space-y-4">
                      <div>
                        <p className="eyebrow">Event Journal</p>
                        <h2 className="mt-2 font-display text-2xl font-semibold text-ink">Snapshot</h2>
                        <p className="mt-2 text-sm leading-6 text-shell-muted">Persisted events for the selected workflow, newest first.</p>
                      </div>
                      <LiveEventList
                        events={eventsQuery.data?.items ?? []}
                        emptyTitle="No workflow events yet"
                        emptyBody="Pick a workflow with persisted events or launch a new run."
                      />
                    </div>

                    <div className="space-y-4">
                      <div>
                        <p className="eyebrow">SSE Stream</p>
                        <h2 className="mt-2 font-display text-2xl font-semibold text-ink">Live feed</h2>
                        <p className="mt-2 text-sm leading-6 text-shell-muted">
                          {streamConnected
                            ? "Connected. Incoming persona and task events land here immediately."
                            : selectedWorkflow && !isWorkflowTerminal(selectedWorkflow.status)
                              ? "Auto-connects to active workflows. Toggle the stream below."
                              : "Select a running workflow to attach a live event feed."}
                        </p>
                      </div>
                      <div className="flex flex-wrap gap-3">
                        <button
                          type="button"
                          onClick={() => setStreaming((current) => !current)}
                          className={secondaryButtonClassName()}
                        >
                          <span className="inline-flex items-center gap-2">
                            <Radio className="h-4 w-4" />
                            {streaming ? "Pause stream" : "Connect stream"}
                          </span>
                        </button>
                      </div>
                      <LiveEventList
                        events={streamEvents}
                        emptyTitle="No live events yet"
                        emptyBody="Pick a running workflow or connect the stream to watch events arrive in real time."
                      />
                    </div>
                  </div>
                </div>
              ) : null}

              {/* ── Document tab ─────────────────────────────────────────────── */}
              {studioTab === "document" ? (
                <div className="space-y-4">
                  {selectedWorkflow ? (
                    <WorkflowDocument workflow={selectedWorkflow} />
                  ) : (
                    <EmptyState title="No workflow selected" body="Pick a workflow from the selector on the left." />
                  )}
                </div>
              ) : null}

            </div>
          </div>
        </div>
      </Surface>
    </div>
  );
}