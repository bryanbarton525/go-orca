"use client";

import { useQuery } from "@tanstack/react-query";
import { Boxes, Cable, Radar, Sparkles } from "lucide-react";
import { useOrcaWorkspace } from "./orca-workspace-provider";
import { EmptyState, SectionIntro, StatusBadge, Surface } from "./ui";
import { getHealthz, getReadyz, listProviders, listWorkflows, resolveCustomizations } from "../lib/orca/api";
import { formatRelative } from "../lib/orca/presentation";

function StatTile({
  label,
  value,
  description,
  icon: Icon,
}: {
  label: string;
  value: string;
  description: string;
  icon: typeof Radar;
}) {
  return (
    <Surface className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="eyebrow">{label}</p>
        <div className="rounded-full bg-shell-panel/85 p-3 text-lagoon">
          <Icon className="h-5 w-5" />
        </div>
      </div>
      <div>
        <p className="font-display text-3xl font-semibold text-ink">{value}</p>
        <p className="mt-2 text-sm text-shell-muted">{description}</p>
      </div>
    </Surface>
  );
}

export function OverviewDashboard() {
  const workspace = useOrcaWorkspace();

  const healthQuery = useQuery({ queryKey: ["healthz"], queryFn: getHealthz, refetchInterval: 30_000 });
  const readyQuery = useQuery({ queryKey: ["readyz"], queryFn: getReadyz, refetchInterval: 30_000 });
  const providersQuery = useQuery({ queryKey: ["providers"], queryFn: listProviders });
  const workflowsQuery = useQuery({
    queryKey: ["overview-workflows", workspace.tenantId, workspace.scopeId],
    queryFn: () => listWorkflows(workspace, 6, 0),
  });
  const customizationsQuery = useQuery({
    queryKey: ["overview-customizations", workspace.tenantId, workspace.scopeId],
    queryFn: () => resolveCustomizations(workspace),
  });

  const providers = providersQuery.data ?? [];
  const recentWorkflows = workflowsQuery.data?.items ?? [];
  const customizationTotal =
    (customizationsQuery.data?.skills.length ?? 0) +
    (customizationsQuery.data?.agents.length ?? 0) +
    (customizationsQuery.data?.prompts.length ?? 0);

  return (
    <div className="space-y-6 pb-28 lg:pb-8">
      <Surface className="space-y-6 overflow-hidden">
        <SectionIntro
          eyebrow="Operations Snapshot"
          title="One view over your orchestration surface"
          description="The dashboard emphasizes runtime posture first: service readiness, current context, provider availability, active customization overlays, and recent workflow movement."
          actions={<StatusBadge status={readyQuery.data?.status ?? healthQuery.data?.status} />}
        />

        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <StatTile
            label="API Liveness"
            value={(healthQuery.data?.status ?? "unknown").toUpperCase()}
            description="Unauthenticated probe response from the protected proxy."
            icon={Radar}
          />
          <StatTile
            label="Readiness"
            value={(readyQuery.data?.status ?? "unknown").toUpperCase()}
            description="Database and provider readiness from go-orca."
            icon={Sparkles}
          />
          <StatTile
            label="Providers"
            value={String(providers.length)}
            description="Registered model backends surfaced by the live API."
            icon={Boxes}
          />
          <StatTile
            label="Recent Workflows"
            value={String(recentWorkflows.length)}
            description="Newest visible runs in the current tenant context."
            icon={Cable}
          />
        </div>
      </Surface>

      <div className="grid gap-4 xl:grid-cols-[1.2fr_0.8fr]">
        <Surface className="space-y-4">
          <div>
            <p className="eyebrow">Recent Activity</p>
            <h2 className="mt-2 font-display text-2xl font-semibold text-ink">Workflow pulse</h2>
          </div>
          {recentWorkflows.length === 0 ? (
            <EmptyState title="No workflows yet" body="Create a workflow from the Workflows screen to start populating the journal." />
          ) : (
            <div className="space-y-3">
              {recentWorkflows.map((workflow) => (
                <div
                  key={workflow.id}
                  className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4"
                >
                  <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
                    <div>
                      <p className="text-sm font-semibold text-ink">{workflow.title || workflow.request || workflow.id}</p>
                      <p className="mt-1 text-xs text-shell-soft">
                        {workflow.provider_name || "provider pending"}
                        {workflow.model_name ? ` / ${workflow.model_name}` : ""}
                      </p>
                    </div>
                    <div className="flex items-center gap-3">
                      <StatusBadge status={workflow.status} />
                      <span className="text-xs text-shell-soft">{formatRelative(workflow.updated_at ?? workflow.created_at)}</span>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Surface>

        <div className="space-y-4">
          <Surface className="space-y-4">
            <div>
              <p className="eyebrow">Active Context</p>
              <h2 className="mt-2 font-display text-2xl font-semibold text-ink">Routing headers</h2>
            </div>
            <div className="space-y-3 rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4 text-sm text-shell-muted">
              <div>
                <p className="text-xs font-semibold uppercase tracking-[0.16em] text-shell-soft">Tenant</p>
                <p className="mt-1 break-all font-medium text-ink">{workspace.tenantId || "Using server default tenant"}</p>
              </div>
              <div>
                <p className="text-xs font-semibold uppercase tracking-[0.16em] text-shell-soft">Scope</p>
                <p className="mt-1 break-all font-medium text-ink">{workspace.scopeId || "Using server default scope"}</p>
              </div>
            </div>
          </Surface>

          <Surface className="space-y-4">
            <div>
              <p className="eyebrow">Customization Chain</p>
              <h2 className="mt-2 font-display text-2xl font-semibold text-ink">Resolved overlays</h2>
            </div>
            <div className="grid gap-3 sm:grid-cols-3">
              <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Skills</p>
                <p className="mt-3 font-display text-3xl font-semibold text-ink">{customizationsQuery.data?.skills.length ?? 0}</p>
              </div>
              <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Agents</p>
                <p className="mt-3 font-display text-3xl font-semibold text-ink">{customizationsQuery.data?.agents.length ?? 0}</p>
              </div>
              <div className="rounded-3xl border border-shell-border/40 bg-shell-panel/80 p-4">
                <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Prompts</p>
                <p className="mt-3 font-display text-3xl font-semibold text-ink">{customizationsQuery.data?.prompts.length ?? 0}</p>
              </div>
            </div>
            <p className="text-sm text-shell-muted">
              {customizationTotal > 0
                ? `The current scope resolves ${customizationTotal} active customization items.`
                : customizationsQuery.data?.note || "No custom overlays resolved for the current scope."}
            </p>
          </Surface>
        </div>
      </div>
    </div>
  );
}