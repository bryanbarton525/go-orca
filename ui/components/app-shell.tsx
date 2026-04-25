"use client";

import Image from "next/image";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Activity, Boxes, Cable, ChevronRight, House, Plug, ShieldEllipsis } from "lucide-react";
import { LogoutButton } from "./logout-button";
import { ThemeToggle } from "./theme-toggle";
import { Surface, textFieldClassName } from "./ui";
import { useOrcaWorkspace } from "./orca-workspace-provider";

const navigation = [
  { href: "/overview", label: "Overview", icon: House },
  { href: "/workflows", label: "Workflows", icon: Cable },
  { href: "/providers", label: "Providers", icon: Boxes },
  { href: "/administration", label: "Admin", icon: ShieldEllipsis },
  { href: "/mcp", label: "MCP", icon: Plug },
  { href: "/health", label: "Health", icon: Activity },
] as const;

function WorkspaceControls() {
  const { tenantId, scopeId, setTenantId, setScopeId, resetWorkspace } = useOrcaWorkspace();

  return (
    <div className="space-y-3 rounded-[1.5rem] border border-shell-border/40 bg-shell-panel/75 p-4">
      <div className="flex items-center justify-between">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Request Context</p>
          <p className="mt-1 text-xs text-shell-soft">Leave blank to use go-orca server defaults.</p>
        </div>
        <button
          type="button"
          onClick={resetWorkspace}
          className="text-xs font-semibold uppercase tracking-[0.16em] text-shell-soft transition hover:text-lagoon"
        >
          Reset
        </button>
      </div>
      <label className="block text-xs font-medium text-ink">
        Tenant ID
        <input
          value={tenantId}
          onChange={(event) => setTenantId(event.target.value)}
          placeholder="server default or tenant uuid"
          className={`${textFieldClassName()} mt-2 text-xs`}
        />
      </label>
      <label className="block text-xs font-medium text-ink">
        Scope ID
        <input
          value={scopeId}
          onChange={(event) => setScopeId(event.target.value)}
          placeholder="server default or scope uuid"
          className={`${textFieldClassName()} mt-2 text-xs`}
        />
      </label>
    </div>
  );
}

export function AppShell({ children, userName }: { children: React.ReactNode; userName: string }) {
  const pathname = usePathname();

  return (
    <div className="min-h-screen px-4 py-4 md:px-6">
      <div className="mx-auto grid max-w-[1600px] gap-4 lg:grid-cols-[300px_minmax(0,1fr)]">
        <aside className="hidden lg:block">
          <Surface className="sticky top-4 space-y-6 p-6">
            <div className="space-y-3">
              <div className="flex items-center gap-3">
                <Image
                  src="/go-orca.png"
                  alt="go-orca"
                  width={52}
                  height={52}
                  className="rounded-xl"
                  priority
                />
                <div className="inline-flex items-center gap-2 rounded-full border border-shell-border/40 bg-shell-panel/85 px-3 py-1 text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">
                  <span className="h-2 w-2 rounded-full bg-ember" />
                  go-orca UI
                </div>
              </div>
              <div>
                <h1 className="font-display text-3xl font-semibold tracking-tight text-ink">Control Center</h1>
                <p className="mt-2 text-sm leading-6 text-shell-muted">
                  Authenticated orchestration console for workflows, providers, tenants, scopes, and runtime health.
                </p>
              </div>
            </div>

            <nav className="space-y-2">
              {navigation.map((item) => {
                const Icon = item.icon;
                const active = pathname === item.href;
                return (
                  <Link
                    key={item.href}
                    href={item.href}
                    className={`flex items-center justify-between rounded-2xl px-4 py-3 text-sm font-medium transition ${
                      active
                        ? "bg-lagoon text-white shadow-lg"
                        : "bg-shell-panel/75 text-ink hover:bg-shell-panel hover:text-lagoon"
                    }`}
                  >
                    <span className="flex items-center gap-3">
                      <Icon className="h-4 w-4" />
                      {item.label}
                    </span>
                    <ChevronRight className="h-4 w-4 opacity-70" />
                  </Link>
                );
              })}
            </nav>

            <WorkspaceControls />

            <div className="rounded-[1.5rem] border border-shell-border/40 bg-shell-panel/75 p-4">
              <p className="text-xs font-semibold uppercase tracking-[0.16em] text-lagoon">Authenticated</p>
              <p className="mt-2 text-sm font-medium text-ink">{userName}</p>
              <p className="mt-1 text-xs text-shell-soft">Session enforced with Authentik via OIDC and MFA.</p>
              <div className="mt-4 flex flex-wrap gap-3">
                <ThemeToggle />
                <LogoutButton />
              </div>
            </div>
          </Surface>
        </aside>

        <div className="space-y-4">
          <Surface className="sticky top-4 z-20 p-4 lg:hidden">
            <div className="flex items-center justify-between gap-4">
              <div>
                <p className="eyebrow">go-orca UI</p>
                <p className="font-display text-2xl font-semibold text-ink">Control Center</p>
              </div>
              <div className="flex items-center gap-2">
                <ThemeToggle />
                <LogoutButton />
              </div>
            </div>
            <div className="mt-4">
              <WorkspaceControls />
            </div>
          </Surface>

          <main>{children}</main>

          <Surface className="fixed inset-x-4 bottom-4 z-20 p-2 lg:hidden">
            <nav className="grid grid-cols-5 gap-2">
              {navigation.map((item) => {
                const Icon = item.icon;
                const active = pathname === item.href;
                return (
                  <Link
                    key={item.href}
                    href={item.href}
                    className={`flex flex-col items-center gap-1 rounded-2xl px-2 py-3 text-[11px] font-semibold uppercase tracking-[0.12em] transition ${
                      active ? "bg-lagoon text-white" : "text-shell-muted"
                    }`}
                  >
                    <Icon className="h-4 w-4" />
                    {item.label}
                  </Link>
                );
              })}
            </nav>
          </Surface>
        </div>
      </div>
    </div>
  );
}