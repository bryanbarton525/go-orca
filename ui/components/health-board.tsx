"use client";

import { useQuery } from "@tanstack/react-query";
import { CircleCheckBig, LifeBuoy } from "lucide-react";
import { getHealthz, getReadyz } from "../lib/orca/api";
import { JsonCard, SectionIntro, StatusBadge, Surface } from "./ui";

export function HealthBoard() {
  const healthQuery = useQuery({ queryKey: ["healthz"], queryFn: getHealthz, refetchInterval: 10_000 });
  const readyQuery = useQuery({ queryKey: ["readyz"], queryFn: getReadyz, refetchInterval: 10_000 });

  return (
    <div className="space-y-6 pb-28 lg:pb-8">
      <Surface className="space-y-6">
        <SectionIntro
          eyebrow="Service Health"
          title="Operational probes"
          description="These cards expose the same liveness and readiness endpoints defined in the go-orca API. They are polled continuously so you can spot degraded upstream state quickly."
          actions={<StatusBadge status={readyQuery.data?.status ?? healthQuery.data?.status} />}
        />

        <div className="grid gap-4 md:grid-cols-2">
          <div className="rounded-[1.75rem] border border-shell-border/40 bg-shell-panel/80 p-5">
            <div className="flex items-center justify-between gap-3">
              <div>
                <p className="eyebrow">Liveness</p>
                <h2 className="mt-2 font-display text-2xl font-semibold text-ink">/healthz</h2>
              </div>
              <div className="rounded-full bg-shell-panel/85 p-3 text-lagoon">
                <LifeBuoy className="h-5 w-5" />
              </div>
            </div>
            <p className="mt-4 text-sm text-shell-muted">
              Should return immediately without touching downstream dependencies.
            </p>
            <div className="mt-4">
              <StatusBadge status={healthQuery.data?.status} />
            </div>
          </div>

          <div className="rounded-[1.75rem] border border-shell-border/40 bg-shell-panel/80 p-5">
            <div className="flex items-center justify-between gap-3">
              <div>
                <p className="eyebrow">Readiness</p>
                <h2 className="mt-2 font-display text-2xl font-semibold text-ink">/readyz</h2>
              </div>
              <div className="rounded-full bg-shell-panel/85 p-3 text-lagoon">
                <CircleCheckBig className="h-5 w-5" />
              </div>
            </div>
            <p className="mt-4 text-sm text-shell-muted">
              Validates store connectivity and provider health from the live process.
            </p>
            <div className="mt-4">
              <StatusBadge status={readyQuery.data?.status} />
            </div>
          </div>
        </div>

        <div className="grid gap-4 xl:grid-cols-2">
          <JsonCard title="Liveness payload" value={healthQuery.data ?? { status: "loading" }} />
          <JsonCard title="Readiness payload" value={readyQuery.data ?? { status: "loading" }} />
        </div>
      </Surface>
    </div>
  );
}