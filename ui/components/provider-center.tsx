"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Radar } from "lucide-react";
import { listProviderModels, listProviders, testProvider } from "../lib/orca/api";
import { formatDate } from "../lib/orca/presentation";
import type { ProviderInfo, ProviderTestResult } from "../types/orca";
import { EmptyState, SectionIntro, StatusBadge, Surface, secondaryButtonClassName } from "./ui";

type TestedProviderResult = ProviderTestResult & { testedAt: string };

export function ProviderCenter() {
  const providersQuery = useQuery({ queryKey: ["providers"], queryFn: listProviders });
  const [results, setResults] = useState<Record<string, TestedProviderResult>>({});

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
            {providers.map((provider) => (
              <ProviderCard
                key={provider.name}
                provider={provider}
                latestResult={results[provider.name]}
                probePending={testMutation.isPending}
                onProbe={(providerName) => testMutation.mutate(providerName)}
              />
            ))}
          </div>
        )}
      </Surface>
    </div>
  );
}

function ProviderCard({
  provider,
  latestResult,
  probePending,
  onProbe,
}: {
  provider: ProviderInfo;
  latestResult?: TestedProviderResult;
  probePending: boolean;
  onProbe: (providerName: string) => void;
}) {
  const modelsQuery = useQuery({
    queryKey: ["provider-models", provider.name],
    queryFn: () => listProviderModels(provider.name),
    staleTime: 60_000,
  });
  const healthy = latestResult?.ok ?? latestResult?.healthy;
  const models = modelsQuery.data?.items ?? [];
  const visibleModels = models.slice(0, 8);

  return (
    <div className="rounded-[1.75rem] border border-shell-border/40 bg-shell-panel/80 p-5">
      <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
        <div className="space-y-2">
          <p className="eyebrow">{provider.name}</p>
          <h2 className="font-display text-2xl font-semibold text-ink">{provider.name}</h2>
          <p className="text-sm text-shell-muted">
            {provider.default_model
              ? `Default model: ${provider.default_model}`
              : "Provider default model is controlled by server configuration."}
          </p>
          {provider.capabilities?.length ? (
            <div className="flex flex-wrap gap-2">
              {provider.capabilities.map((capability) => (
                <span
                  key={capability}
                  className="rounded-full border border-shell-border/40 bg-shell-subtle px-2.5 py-1 text-xs font-medium text-shell-muted"
                >
                  {capability}
                </span>
              ))}
            </div>
          ) : null}
        </div>
        <button
          type="button"
          onClick={() => onProbe(provider.name)}
          disabled={probePending}
          className={secondaryButtonClassName()}
        >
          <span className="inline-flex items-center gap-2">
            <Radar className="h-4 w-4" />
            Run probe
          </span>
        </button>
      </div>

      <div className="mt-5 grid gap-4 lg:grid-cols-2">
        <div className="rounded-3xl border border-shell-border/40 bg-shell-subtle p-4">
          <div className="flex items-center justify-between gap-3">
            <p className="text-sm font-semibold text-ink">Model inventory</p>
            <StatusBadge status={modelsQuery.isLoading ? "running" : modelsQuery.isError ? "failed" : "completed"} />
          </div>
          {modelsQuery.isError ? (
            <p className="mt-3 text-sm text-shell-danger-text">
              Models could not be loaded. Workflows can still use a manually entered model name.
            </p>
          ) : models.length > 0 ? (
            <div className="mt-3 space-y-2 text-sm text-shell-muted">
              <p>
                {models.length} model{models.length === 1 ? "" : "s"} advertised
              </p>
              <div className="flex flex-wrap gap-2">
                {visibleModels.map((model) => (
                  <span
                    key={model.id}
                    title={model.description}
                    className="rounded-full border border-shell-border/40 bg-shell-panel/70 px-2.5 py-1 text-xs font-medium text-ink"
                  >
                    {model.name || model.id}
                  </span>
                ))}
                {models.length > visibleModels.length ? (
                  <span className="rounded-full border border-shell-border/40 bg-shell-panel/70 px-2.5 py-1 text-xs font-medium text-shell-muted">
                    +{models.length - visibleModels.length} more
                  </span>
                ) : null}
              </div>
            </div>
          ) : (
            <p className="mt-3 text-sm text-shell-soft">
              {modelsQuery.isLoading ? "Loading models..." : "No models were advertised by this provider."}
            </p>
          )}
        </div>

        <div className="rounded-3xl border border-shell-border/40 bg-shell-subtle p-4">
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
    </div>
  );
}
