"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Radar } from "lucide-react";
import { listProviders, testProvider } from "../lib/orca/api";
import { formatDate } from "../lib/orca/presentation";
import type { ProviderTestResult } from "../types/orca";
import { EmptyState, SectionIntro, StatusBadge, Surface, secondaryButtonClassName } from "./ui";

export function ProviderCenter() {
  const providersQuery = useQuery({ queryKey: ["providers"], queryFn: listProviders });
  const [results, setResults] = useState<Record<string, ProviderTestResult & { testedAt: string }>>({});

  const testMutation = useMutation({
    mutationFn: (providerName: string) => testProvider(providerName),
    onSuccess: (result, providerName) => {
      setResults((current) => ({
        ...current,
        [providerName]: {
          ...result,
          testedAt: new Date().toISOString(),
        },
      }));
    },
  });

  const providers = useMemo(() => providersQuery.data ?? [], [providersQuery.data]);

  return (
    <div className="space-y-6 pb-28 lg:pb-8">
      <Surface className="space-y-6">
        <SectionIntro
          eyebrow="Provider Operations"
          title="Inspect and probe model backends"
          description="The provider page covers every provider-facing API action: inventory the currently registered backends and run on-demand health checks without leaving the UI."
          actions={<StatusBadge status={providersQuery.isFetching ? "running" : "ready"} />}
        />

        {providers.length === 0 ? (
          <EmptyState title="No providers returned" body="Enable at least one provider in go-orca before testing connectivity here." />
        ) : (
          <div className="grid gap-4 xl:grid-cols-2">
            {providers.map((provider) => {
              const latestResult = results[provider.name];
              const healthy = latestResult?.ok ?? latestResult?.healthy;
              return (
                <div key={provider.name} className="rounded-[1.75rem] border border-shell-border/40 bg-shell-panel/80 p-5">
                  <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
                    <div className="space-y-2">
                      <p className="eyebrow">{provider.name}</p>
                      <h2 className="font-display text-2xl font-semibold text-ink">{provider.name}</h2>
                      <p className="text-sm text-shell-muted">
                        {provider.capabilities?.length
                          ? `Capabilities: ${provider.capabilities.join(", ")}`
                          : provider.default_model
                            ? `Default model: ${provider.default_model}`
                            : "Capability metadata is not advertised by this provider implementation."}
                      </p>
                    </div>
                    <button
                      type="button"
                      onClick={() => testMutation.mutate(provider.name)}
                      disabled={testMutation.isPending}
                      className={secondaryButtonClassName()}
                    >
                      <span className="inline-flex items-center gap-2">
                        <Radar className="h-4 w-4" />
                        Run probe
                      </span>
                    </button>
                  </div>

                  <div className="mt-5 rounded-3xl border border-shell-border/40 bg-shell-subtle p-4">
                    <div className="flex items-center justify-between gap-3">
                      <p className="text-sm font-semibold text-ink">Latest test result</p>
                      <StatusBadge status={healthy === undefined ? "idle" : healthy ? "completed" : "failed"} />
                    </div>
                    {latestResult ? (
                      <div className="mt-3 space-y-2 text-sm text-shell-muted">
                        <p>
                          Tested at <span className="font-medium text-ink">{formatDate(latestResult.testedAt)}</span>
                        </p>
                        {typeof latestResult.latency_ms === "number" ? (
                          <p>
                            Reported latency <span className="font-medium text-ink">{latestResult.latency_ms} ms</span>
                          </p>
                        ) : null}
                        {latestResult.error ? <p className="text-shell-danger-text">{latestResult.error}</p> : null}
                      </div>
                    ) : (
                      <p className="mt-3 text-sm text-shell-soft">No probe has been run in this browser session yet.</p>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </Surface>
    </div>
  );
}