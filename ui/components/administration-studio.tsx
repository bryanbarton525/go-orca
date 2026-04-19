"use client";

import { useDeferredValue, useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useOrcaWorkspace } from "./orca-workspace-provider";
import {
  createScope,
  createTenant,
  deleteScope,
  deleteTenant,
  getEffectiveConfig,
  getTenant,
  listScopesForTenant,
  listTenants,
  resolveCustomizations,
  updateScope,
  updateTenant,
} from "../lib/orca/api";
import { formatDate, scopeKinds } from "../lib/orca/presentation";
import {
  EmptyState,
  InputLabel,
  JsonCard,
  SectionIntro,
  StatusBadge,
  Surface,
  primaryButtonClassName,
  secondaryButtonClassName,
  textFieldClassName,
} from "./ui";

export function AdministrationStudio() {
  const queryClient = useQueryClient();
  const workspace = useOrcaWorkspace();

  const [tenantSearch, setTenantSearch] = useState("");
  const deferredTenantSearch = useDeferredValue(tenantSearch);
  const [selectedTenantId, setSelectedTenantId] = useState(workspace.tenantId);
  const [selectedScopeId, setSelectedScopeId] = useState(workspace.scopeId);
  const [tenantForm, setTenantForm] = useState({ slug: "", name: "" });
  const [scopeForm, setScopeForm] = useState({ kind: "team", name: "", slug: "", parent_scope_id: "" });
  const [tenantEdit, setTenantEdit] = useState({ slug: "", name: "" });
  const [scopeEdit, setScopeEdit] = useState({ name: "", slug: "" });
  const [tenantMessage, setTenantMessage] = useState<string | null>(null);
  const [scopeMessage, setScopeMessage] = useState<string | null>(null);

  const tenantsQuery = useQuery({ queryKey: ["tenants"], queryFn: listTenants });
  const tenants = useMemo(() => tenantsQuery.data?.items ?? [], [tenantsQuery.data?.items]);

  useEffect(() => {
    if (!selectedTenantId && tenants.length > 0) {
      setSelectedTenantId(tenants[0].id);
    }
  }, [selectedTenantId, tenants]);

  const tenantDetailsQuery = useQuery({
    queryKey: ["tenant", selectedTenantId],
    queryFn: () => getTenant(selectedTenantId),
    enabled: Boolean(selectedTenantId),
  });

  const scopesQuery = useQuery({
    queryKey: ["scopes", selectedTenantId],
    queryFn: () => listScopesForTenant(selectedTenantId),
    enabled: Boolean(selectedTenantId),
  });

  const customizationsQuery = useQuery({
    queryKey: ["customizations", workspace.tenantId, workspace.scopeId],
    queryFn: () => resolveCustomizations(workspace),
  });

  const effectiveConfigQuery = useQuery({
    queryKey: ["effective-config", selectedScopeId, workspace.tenantId],
    queryFn: () => getEffectiveConfig(selectedScopeId, workspace),
    enabled: Boolean(selectedScopeId),
  });

  useEffect(() => {
    const details = tenantDetailsQuery.data;
    if (details) {
      setTenantEdit({ slug: details.slug, name: details.name });
    }
  }, [tenantDetailsQuery.data]);

  useEffect(() => {
    const scope = scopesQuery.data?.items.find((item) => item.id === selectedScopeId);
    if (scope) {
      setScopeEdit({ name: scope.name ?? "", slug: scope.slug ?? "" });
    }
  }, [scopesQuery.data?.items, selectedScopeId]);

  const filteredTenants = useMemo(() => {
    const needle = deferredTenantSearch.trim().toLowerCase();
    if (!needle) {
      return tenants;
    }

    return tenants.filter((tenant) => `${tenant.name} ${tenant.slug}`.toLowerCase().includes(needle));
  }, [deferredTenantSearch, tenants]);

  const selectedScope = scopesQuery.data?.items.find((scope) => scope.id === selectedScopeId) ?? null;

  const refreshTenants = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ["tenants"] }),
      queryClient.invalidateQueries({ queryKey: ["tenant"] }),
      queryClient.invalidateQueries({ queryKey: ["scopes"] }),
    ]);
  };

  const createTenantMutation = useMutation({
    mutationFn: () => createTenant(tenantForm),
    onSuccess: async (tenant) => {
      setTenantForm({ slug: "", name: "" });
      setSelectedTenantId(tenant.id);
      setTenantMessage(`Created tenant ${tenant.name}.`);
      await refreshTenants();
    },
    onError: (error) => setTenantMessage(error instanceof Error ? error.message : "Failed to create tenant"),
  });

  const updateTenantMutation = useMutation({
    mutationFn: () => updateTenant(selectedTenantId, tenantEdit),
    onSuccess: async (tenant) => {
      setTenantMessage(`Updated tenant ${tenant.name}.`);
      await refreshTenants();
    },
    onError: (error) => setTenantMessage(error instanceof Error ? error.message : "Failed to update tenant"),
  });

  const deleteTenantMutation = useMutation({
    mutationFn: async () => {
      await deleteTenant(selectedTenantId);
    },
    onSuccess: async () => {
      setTenantMessage("Tenant deleted.");
      setSelectedTenantId("");
      setSelectedScopeId("");
      await refreshTenants();
    },
    onError: (error) => setTenantMessage(error instanceof Error ? error.message : "Failed to delete tenant"),
  });

  const createScopeMutation = useMutation({
    mutationFn: () =>
      createScope(selectedTenantId, {
        kind: scopeForm.kind,
        name: scopeForm.name,
        slug: scopeForm.slug,
        parent_scope_id: scopeForm.parent_scope_id || undefined,
      }),
    onSuccess: async (scope) => {
      setScopeForm({ kind: scopeForm.kind, name: "", slug: "", parent_scope_id: "" });
      setSelectedScopeId(scope.id);
      setScopeMessage(`Created scope ${scope.name ?? scope.id}.`);
      await queryClient.invalidateQueries({ queryKey: ["scopes", selectedTenantId] });
    },
    onError: (error) => setScopeMessage(error instanceof Error ? error.message : "Failed to create scope"),
  });

  const updateScopeMutation = useMutation({
    mutationFn: () => updateScope(selectedTenantId, selectedScopeId, scopeEdit),
    onSuccess: async () => {
      setScopeMessage("Scope updated.");
      await queryClient.invalidateQueries({ queryKey: ["scopes", selectedTenantId] });
      await queryClient.invalidateQueries({ queryKey: ["effective-config", selectedScopeId, workspace.tenantId] });
    },
    onError: (error) => setScopeMessage(error instanceof Error ? error.message : "Failed to update scope"),
  });

  const deleteScopeMutation = useMutation({
    mutationFn: () => deleteScope(selectedTenantId, selectedScopeId),
    onSuccess: async () => {
      setScopeMessage("Scope deleted.");
      setSelectedScopeId("");
      await queryClient.invalidateQueries({ queryKey: ["scopes", selectedTenantId] });
      await queryClient.invalidateQueries({ queryKey: ["effective-config"] });
    },
    onError: (error) => setScopeMessage(error instanceof Error ? error.message : "Failed to delete scope"),
  });

  return (
    <div className="space-y-6 pb-28 lg:pb-8">
      <Surface className="space-y-6">
        <SectionIntro
          eyebrow="Tenants, Scopes, and Resolution"
          title="Administration surface"
          description="This screen exposes the rest of the API surface: list, create, inspect, update, and delete tenants and scopes, then inspect effective scope config and resolved customizations against the active request context."
          actions={<StatusBadge status={selectedScopeId ? "ready" : "pending"} />}
        />

        <div className="grid gap-4 xl:grid-cols-[0.9fr_1.1fr]">
          <div className="space-y-4">
            <Surface className="space-y-4">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <p className="eyebrow">Tenants</p>
                  <h2 className="mt-2 font-display text-2xl font-semibold text-ink">Inventory</h2>
                </div>
                <input
                  value={tenantSearch}
                  onChange={(event) => setTenantSearch(event.target.value)}
                  placeholder="Filter tenants"
                  className={`${textFieldClassName()} max-w-48 text-sm`}
                />
              </div>

              <div className="space-y-3">
                {filteredTenants.length === 0 ? (
                  <EmptyState title="No tenants found" body="Create the first tenant below or relax the filter." />
                ) : (
                  filteredTenants.map((tenant) => (
                    <button
                      key={tenant.id}
                      type="button"
                      onClick={() => setSelectedTenantId(tenant.id)}
                      className={`w-full rounded-3xl border p-4 text-left transition ${
                        selectedTenantId === tenant.id
                          ? "border-lagoon bg-[rgba(15,108,116,0.08)]"
                          : "border-shell-border/40 bg-shell-panel/80 hover:border-lagoon"
                      }`}
                    >
                      <p className="text-sm font-semibold text-ink">{tenant.name}</p>
                      <p className="mt-1 text-xs text-shell-soft">{tenant.slug}</p>
                    </button>
                  ))
                )}
              </div>
            </Surface>

            <Surface className="space-y-4">
              <div>
                <p className="eyebrow">Create Tenant</p>
                <h2 className="mt-2 font-display text-2xl font-semibold text-ink">New tenant</h2>
              </div>
              <div className="grid gap-3 md:grid-cols-2">
                <InputLabel label="Slug">
                  <input
                    value={tenantForm.slug}
                    onChange={(event) => setTenantForm((current) => ({ ...current, slug: event.target.value }))}
                    className={textFieldClassName()}
                  />
                </InputLabel>
                <InputLabel label="Name">
                  <input
                    value={tenantForm.name}
                    onChange={(event) => setTenantForm((current) => ({ ...current, name: event.target.value }))}
                    className={textFieldClassName()}
                  />
                </InputLabel>
              </div>
              <div className="flex items-center gap-3">
                <button
                  type="button"
                  onClick={() => createTenantMutation.mutate()}
                  disabled={!tenantForm.slug || !tenantForm.name || createTenantMutation.isPending}
                  className={primaryButtonClassName()}
                >
                  Create tenant
                </button>
                {tenantMessage ? <p className="text-sm text-shell-muted">{tenantMessage}</p> : null}
              </div>
            </Surface>
          </div>

          <div className="space-y-4">
            <Surface className="space-y-4">
              <div>
                <p className="eyebrow">Tenant Details</p>
                <h2 className="mt-2 font-display text-2xl font-semibold text-ink">Inspect and update</h2>
              </div>
              {tenantDetailsQuery.data ? (
                <>
                  <div className="grid gap-3 md:grid-cols-2">
                    <InputLabel label="Tenant name">
                      <input
                        value={tenantEdit.name}
                        onChange={(event) => setTenantEdit((current) => ({ ...current, name: event.target.value }))}
                        className={textFieldClassName()}
                      />
                    </InputLabel>
                    <InputLabel label="Tenant slug">
                      <input
                        value={tenantEdit.slug}
                        onChange={(event) => setTenantEdit((current) => ({ ...current, slug: event.target.value }))}
                        className={textFieldClassName()}
                      />
                    </InputLabel>
                  </div>
                  <div className="flex flex-wrap items-center gap-3 text-sm text-shell-muted">
                    <span>Created {formatDate(tenantDetailsQuery.data.created_at)}</span>
                    <span>Updated {formatDate(tenantDetailsQuery.data.updated_at)}</span>
                  </div>
                  <div className="flex flex-wrap gap-3">
                    <button type="button" onClick={() => updateTenantMutation.mutate()} className={primaryButtonClassName()}>
                      Save tenant
                    </button>
                    <button
                      type="button"
                      onClick={() => workspace.setWorkspace({ tenantId: tenantDetailsQuery.data.id })}
                      className={secondaryButtonClassName()}
                    >
                      Use as active tenant
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        if (window.confirm("Delete this tenant and all of its scopes?")) {
                          deleteTenantMutation.mutate();
                        }
                      }}
                      className="rounded-2xl border border-shell-danger/30 bg-shell-danger/10 px-4 py-2.5 text-sm font-medium text-shell-danger-text"
                    >
                      Delete tenant
                    </button>
                  </div>
                </>
              ) : (
                <EmptyState title="No tenant selected" body="Pick a tenant from the inventory list to inspect it here." />
              )}
            </Surface>

            <Surface className="space-y-4">
              <div>
                <p className="eyebrow">Scopes</p>
                <h2 className="mt-2 font-display text-2xl font-semibold text-ink">Tenant scope graph</h2>
              </div>

              {selectedTenantId ? (
                <>
                  <div className="space-y-3">
                    {(scopesQuery.data?.items ?? []).map((scope) => (
                      <button
                        key={scope.id}
                        type="button"
                        onClick={() => setSelectedScopeId(scope.id)}
                        className={`w-full rounded-3xl border p-4 text-left transition ${
                          selectedScopeId === scope.id
                            ? "border-lagoon bg-[rgba(15,108,116,0.08)]"
                            : "border-shell-border/40 bg-shell-panel/80 hover:border-lagoon"
                        }`}
                      >
                        <div className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
                          <div>
                            <p className="text-sm font-semibold text-ink">{scope.name ?? scope.id}</p>
                            <p className="mt-1 text-xs text-shell-soft">{scope.slug}</p>
                          </div>
                          <StatusBadge status={scope.kind} />
                        </div>
                      </button>
                    ))}
                  </div>

                  <div className="grid gap-3 md:grid-cols-4">
                    <InputLabel label="Kind">
                      <select
                        value={scopeForm.kind}
                        onChange={(event) => setScopeForm((current) => ({ ...current, kind: event.target.value }))}
                        className={textFieldClassName()}
                      >
                        {scopeKinds.map((scopeKind) => (
                          <option key={scopeKind.value} value={scopeKind.value}>
                            {scopeKind.label}
                          </option>
                        ))}
                      </select>
                    </InputLabel>
                    <InputLabel label="Name">
                      <input
                        value={scopeForm.name}
                        onChange={(event) => setScopeForm((current) => ({ ...current, name: event.target.value }))}
                        className={textFieldClassName()}
                      />
                    </InputLabel>
                    <InputLabel label="Slug">
                      <input
                        value={scopeForm.slug}
                        onChange={(event) => setScopeForm((current) => ({ ...current, slug: event.target.value }))}
                        className={textFieldClassName()}
                      />
                    </InputLabel>
                    <InputLabel label="Parent scope id">
                      <input
                        value={scopeForm.parent_scope_id}
                        onChange={(event) => setScopeForm((current) => ({ ...current, parent_scope_id: event.target.value }))}
                        className={textFieldClassName()}
                      />
                    </InputLabel>
                  </div>

                  <div className="flex items-center gap-3">
                    <button type="button" onClick={() => createScopeMutation.mutate()} className={primaryButtonClassName()}>
                      Create scope
                    </button>
                    {scopeMessage ? <p className="text-sm text-shell-muted">{scopeMessage}</p> : null}
                  </div>

                  {selectedScope ? (
                    <div className="rounded-[1.75rem] border border-shell-border/40 bg-shell-subtle p-4">
                      <p className="text-sm font-semibold text-ink">Selected scope</p>
                      <div className="mt-4 grid gap-3 md:grid-cols-2">
                        <InputLabel label="Scope name">
                          <input
                            value={scopeEdit.name}
                            onChange={(event) => setScopeEdit((current) => ({ ...current, name: event.target.value }))}
                            className={textFieldClassName()}
                          />
                        </InputLabel>
                        <InputLabel label="Scope slug">
                          <input
                            value={scopeEdit.slug}
                            onChange={(event) => setScopeEdit((current) => ({ ...current, slug: event.target.value }))}
                            className={textFieldClassName()}
                          />
                        </InputLabel>
                      </div>
                      <div className="mt-4 flex flex-wrap gap-3">
                        <button type="button" onClick={() => updateScopeMutation.mutate()} className={primaryButtonClassName()}>
                          Save scope
                        </button>
                        <button
                          type="button"
                          onClick={() => workspace.setWorkspace({ tenantId: selectedTenantId, scopeId: selectedScope.id })}
                          className={secondaryButtonClassName()}
                        >
                          Use as active context
                        </button>
                        <button
                          type="button"
                          onClick={() => {
                            if (window.confirm("Delete this scope?")) {
                              deleteScopeMutation.mutate();
                            }
                          }}
                          className="rounded-2xl border border-shell-danger/30 bg-shell-danger/10 px-4 py-2.5 text-sm font-medium text-shell-danger-text"
                        >
                          Delete scope
                        </button>
                      </div>
                    </div>
                  ) : null}
                </>
              ) : (
                <EmptyState title="Select a tenant first" body="Scopes are loaded within the currently selected tenant." />
              )}
            </Surface>
          </div>
        </div>

        <div className="grid gap-4 xl:grid-cols-2">
          <JsonCard
            title="Effective scope config"
            value={
              selectedScopeId
                ? {
                    scope: selectedScopeId,
                    resolved_chain: effectiveConfigQuery.data?.resolved_chain,
                    depth: effectiveConfigQuery.data?.depth,
                    effective: effectiveConfigQuery.data?.effective,
                  }
                : { note: "Select a scope to query /scopes/{id}/effective-config" }
            }
          />
          <JsonCard
            title="Resolved customizations"
            value={
              customizationsQuery.data ?? {
                skills: [],
                agents: [],
                prompts: [],
                note: "No customizations resolved for the current context.",
              }
            }
          />
        </div>
      </Surface>
    </div>
  );
}