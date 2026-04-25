"use client";

import { useQuery } from "@tanstack/react-query";
import { CircleAlert, CircleCheck, Layers, Server } from "lucide-react";
import { getMCPRegistry } from "../lib/orca/api";
import type { MCPRegistrySnapshot, MCPServerStatus, MCPToolchainStatus } from "../types/orca";
import { JsonCard, SectionIntro, StatusBadge, Surface } from "./ui";

// MCPRegistryBoard renders the live snapshot of /api/v1/mcp/registry: which
// first-party MCP servers are registered, their connectivity / health, the
// tools they advertise, and the toolchain bindings the engine resolves
// against.  Polls every 15 seconds — the registry's own probes run on a
// similar cadence inside go-orca-api.
export function MCPRegistryBoard() {
  const query = useQuery({
    queryKey: ["mcp", "registry"],
    queryFn: getMCPRegistry,
    refetchInterval: 15_000,
  });

  const data: MCPRegistrySnapshot = query.data ?? { servers: [], toolchains: [] };
  const servers = [...data.servers].sort((a, b) => a.name.localeCompare(b.name));
  const toolchains = [...data.toolchains].sort((a, b) => a.id.localeCompare(b.id));

  const reachable = servers.filter((s) => s.connected).length;
  const total = servers.length;

  return (
    <div className="space-y-6 pb-28 lg:pb-8">
      <Surface className="space-y-6">
        <SectionIntro
          eyebrow="MCP Registry"
          title="First-party MCP servers and toolchains"
          description="Every governed capability the workflow engine invokes is resolved through this registry. Servers are auto-discovered from go-orca.yaml; toolchains bind capability names to advertised tool names. Missing tools are flagged here before a workflow ever runs."
          actions={
            <StatusBadge
              status={total === 0 ? "empty" : reachable === total ? "healthy" : "degraded"}
              label={total === 0 ? "no servers" : `${reachable}/${total} reachable`}
            />
          }
        />

        {query.isError ? (
          <div className="rounded-[1.5rem] border border-rose-300/40 bg-rose-300/10 p-4 text-sm text-rose-200">
            Failed to load MCP registry: {String(query.error)}
          </div>
        ) : null}

        {/* ── Servers ───────────────────────────────────────────────────── */}
        <div>
          <div className="mb-3 flex items-center gap-2 text-sm text-shell-muted">
            <Server className="h-4 w-4 text-lagoon" />
            <span className="font-display text-base font-semibold text-ink">Servers</span>
          </div>
          {servers.length === 0 ? (
            <div className="rounded-[1.5rem] border border-shell-border/40 bg-shell-panel/60 p-5 text-sm text-shell-muted">
              No MCP servers configured. Enable one or more under <code>tools.mcp</code> in
              <code className="mx-1">go-orca.yaml</code> (or via the Helm <code>mcp.&lt;name&gt;.enabled</code> toggles).
            </div>
          ) : (
            <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
              {servers.map((server) => (
                <ServerCard key={server.name} server={server} />
              ))}
            </div>
          )}
        </div>

        {/* ── Toolchains ────────────────────────────────────────────────── */}
        <div>
          <div className="mb-3 flex items-center gap-2 text-sm text-shell-muted">
            <Layers className="h-4 w-4 text-lagoon" />
            <span className="font-display text-base font-semibold text-ink">Toolchains</span>
          </div>
          {toolchains.length === 0 ? (
            <div className="rounded-[1.5rem] border border-shell-border/40 bg-shell-panel/60 p-5 text-sm text-shell-muted">
              No toolchains configured.
            </div>
          ) : (
            <div className="grid gap-4 md:grid-cols-2">
              {toolchains.map((tc) => (
                <ToolchainCard key={tc.id} toolchain={tc} />
              ))}
            </div>
          )}
        </div>

        <JsonCard title="Raw registry snapshot" value={data} />
      </Surface>
    </div>
  );
}

function ServerCard({ server }: { server: MCPServerStatus }) {
  const tone = server.healthy ? "healthy" : server.connected ? "degraded" : "down";
  return (
    <div className="rounded-[1.75rem] border border-shell-border/40 bg-shell-panel/80 p-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="eyebrow">MCP server</p>
          <h3 className="mt-2 font-display text-lg font-semibold text-ink">{server.name}</h3>
          {server.endpoint ? (
            <p className="mt-1 break-all text-xs text-shell-muted">{server.endpoint}</p>
          ) : null}
        </div>
        <div className="rounded-full bg-shell-panel/85 p-2 text-lagoon">
          {server.healthy ? <CircleCheck className="h-4 w-4" /> : <CircleAlert className="h-4 w-4" />}
        </div>
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2 text-xs">
        <StatusBadge status={tone} label={tone} />
        {server.required ? (
          <span className="rounded-full border border-amber-300/40 bg-amber-300/10 px-2 py-0.5 text-amber-200">required</span>
        ) : null}
        {server.transport ? (
          <span className="rounded-full border border-shell-border/40 bg-shell-panel/60 px-2 py-0.5 text-shell-muted">
            {server.transport}
          </span>
        ) : null}
      </div>

      {server.advertised_tools && server.advertised_tools.length > 0 ? (
        <div className="mt-4">
          <p className="text-xs uppercase tracking-wide text-shell-muted">Advertised tools</p>
          <div className="mt-2 flex flex-wrap gap-1">
            {server.advertised_tools.map((tool) => (
              <code
                key={tool}
                className="rounded-md border border-shell-border/40 bg-shell-panel/60 px-1.5 py-0.5 text-[11px] text-shell-muted"
              >
                {tool}
              </code>
            ))}
          </div>
        </div>
      ) : null}

      {server.last_error ? (
        <p className="mt-3 break-words text-xs text-rose-300">{server.last_error}</p>
      ) : null}
    </div>
  );
}

function ToolchainCard({ toolchain }: { toolchain: MCPToolchainStatus }) {
  const profileNames = Object.keys(toolchain.validation_profiles ?? {});
  const missing = toolchain.missing_capabilities ?? [];
  const tone = !toolchain.server_reachable ? "down" : missing.length > 0 ? "degraded" : "healthy";

  return (
    <div className="rounded-[1.75rem] border border-shell-border/40 bg-shell-panel/80 p-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="eyebrow">Toolchain</p>
          <h3 className="mt-2 font-display text-lg font-semibold text-ink">{toolchain.id}</h3>
          <p className="mt-1 text-xs text-shell-muted">
            via <code>{toolchain.mcp_server}</code>
            {toolchain.languages && toolchain.languages.length > 0 ? ` · ${toolchain.languages.join(", ")}` : ""}
          </p>
        </div>
        <StatusBadge status={tone} label={tone} />
      </div>

      {toolchain.capabilities && toolchain.capabilities.length > 0 ? (
        <div className="mt-4">
          <p className="text-xs uppercase tracking-wide text-shell-muted">Capabilities</p>
          <div className="mt-2 flex flex-wrap gap-1">
            {toolchain.capabilities.map((cap) => {
              const tool = toolchain.capability_tools?.[cap] ?? cap;
              const isMissing = missing.includes(cap);
              return (
                <span
                  key={cap}
                  className={
                    "rounded-md border px-1.5 py-0.5 text-[11px] " +
                    (isMissing
                      ? "border-rose-300/40 bg-rose-300/10 text-rose-200"
                      : "border-shell-border/40 bg-shell-panel/60 text-shell-muted")
                  }
                  title={`${cap} → ${tool}`}
                >
                  {cap}
                </span>
              );
            })}
          </div>
        </div>
      ) : null}

      {profileNames.length > 0 ? (
        <p className="mt-3 text-xs text-shell-muted">
          Profiles: {profileNames.join(", ")}
        </p>
      ) : null}

      {toolchain.checkpoint_capability ? (
        <p className="mt-1 text-xs text-shell-muted">
          Checkpoint: <code>{toolchain.checkpoint_capability}</code>
          {toolchain.push_checkpoints ? " (push)" : ""}
        </p>
      ) : null}
    </div>
  );
}
