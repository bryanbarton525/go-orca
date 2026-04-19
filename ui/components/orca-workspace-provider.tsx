"use client";

import {
  createContext,
  startTransition,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import { getWorkspaceDefaults } from "../lib/config/env";

type OrcaWorkspaceState = {
  tenantId: string;
  scopeId: string;
};

type OrcaWorkspaceContextValue = OrcaWorkspaceState & {
  setTenantId: (tenantId: string) => void;
  setScopeId: (scopeId: string) => void;
  setWorkspace: (nextState: Partial<OrcaWorkspaceState>) => void;
  resetWorkspace: () => void;
};

const STORAGE_KEY = "go-orca-ui.workspace";

const OrcaWorkspaceContext = createContext<OrcaWorkspaceContextValue | null>(null);

function persistWorkspace(nextState: OrcaWorkspaceState) {
  if (typeof window === "undefined") {
    return;
  }

  window.localStorage.setItem(STORAGE_KEY, JSON.stringify(nextState));
}

export function OrcaWorkspaceProvider({ children }: { children: React.ReactNode }) {
  const defaults = useMemo(() => getWorkspaceDefaults(), []);
  const [workspace, setWorkspaceState] = useState<OrcaWorkspaceState>(defaults);

  useEffect(() => {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) {
      persistWorkspace(defaults);
      return;
    }

    try {
      const parsed = JSON.parse(raw) as Partial<OrcaWorkspaceState>;
      setWorkspaceState({
        tenantId: typeof parsed.tenantId === "string" ? parsed.tenantId : defaults.tenantId,
        scopeId: typeof parsed.scopeId === "string" ? parsed.scopeId : defaults.scopeId,
      });
    } catch {
      persistWorkspace(defaults);
    }
  }, [defaults]);

  const value = useMemo<OrcaWorkspaceContextValue>(
    () => ({
      ...workspace,
      setTenantId(tenantId: string) {
        startTransition(() => {
          setWorkspaceState((current) => {
            const nextState = { ...current, tenantId };
            persistWorkspace(nextState);
            return nextState;
          });
        });
      },
      setScopeId(scopeId: string) {
        startTransition(() => {
          setWorkspaceState((current) => {
            const nextState = { ...current, scopeId };
            persistWorkspace(nextState);
            return nextState;
          });
        });
      },
      setWorkspace(nextState: Partial<OrcaWorkspaceState>) {
        startTransition(() => {
          setWorkspaceState((current) => {
            const merged = { ...current, ...nextState };
            persistWorkspace(merged);
            return merged;
          });
        });
      },
      resetWorkspace() {
        startTransition(() => {
          setWorkspaceState(defaults);
          persistWorkspace(defaults);
        });
      },
    }),
    [defaults, workspace]
  );

  return <OrcaWorkspaceContext.Provider value={value}>{children}</OrcaWorkspaceContext.Provider>;
}

export function useOrcaWorkspace() {
  const context = useContext(OrcaWorkspaceContext);

  if (!context) {
    throw new Error("useOrcaWorkspace must be used within an OrcaWorkspaceProvider");
  }

  return context;
}