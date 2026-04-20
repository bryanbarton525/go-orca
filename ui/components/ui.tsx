import { useEffect, type ReactNode } from "react";
import { X } from "lucide-react";
import { toneForStatus } from "../lib/orca/presentation";

export function Surface({
  children,
  className = "",
}: {
  children: ReactNode;
  className?: string;
}) {
  return <section className={`glass-panel rounded-[1.75rem] p-5 shadow-aura ${className}`}>{children}</section>;
}

export function SectionIntro({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow: string;
  title: string;
  description: string;
  actions?: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-4 border-b border-shell-border/40 pb-5 md:flex-row md:items-end md:justify-between">
      <div className="space-y-2">
        <p className="eyebrow">{eyebrow}</p>
        <h1 className="font-display text-3xl font-semibold tracking-tight text-ink md:text-4xl">{title}</h1>
        <p className="max-w-3xl text-sm leading-6 text-shell-muted">{description}</p>
      </div>
      {actions}
    </div>
  );
}

export function StatusBadge({ status, label }: { status?: string; label?: string }) {
  return (
    <span
      className={`inline-flex max-w-full shrink-0 whitespace-nowrap rounded-full px-2.5 py-1 leading-none text-xs font-semibold uppercase tracking-[0.14em] ${toneForStatus(status)}`}
    >
      {label ?? status ?? "unknown"}
    </span>
  );
}

export function EmptyState({ title, body }: { title: string; body: string }) {
  return (
    <div className="rounded-3xl border border-dashed border-shell-border/40 bg-shell-panel/60 px-5 py-8 text-center">
      <p className="font-semibold text-ink">{title}</p>
      <p className="mt-2 text-sm text-shell-muted">{body}</p>
    </div>
  );
}

export function JsonCard({ title, value }: { title: string; value: unknown }) {
  return (
    <div className="rounded-3xl border border-shell-border/40 bg-shell-subtle p-4">
      <div className="mb-3 flex items-center justify-between">
        <p className="text-sm font-semibold text-ink">{title}</p>
      </div>
      <pre className="thin-scrollbar overflow-x-auto rounded-2xl bg-shell-code p-4 text-xs leading-6 text-shell-code-text">{JSON.stringify(value, null, 2)}</pre>
    </div>
  );
}

export function InputLabel({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="flex flex-col gap-2 text-sm text-shell-muted">
      <span className="font-medium text-ink">{label}</span>
      {children}
      {hint ? <span className="text-xs text-shell-soft">{hint}</span> : null}
    </label>
  );
}

export function textFieldClassName() {
  return "w-full rounded-2xl border border-shell-border/45 bg-shell-panel/90 px-4 py-3 text-sm text-ink outline-none transition focus:border-lagoon focus:ring-2 focus:ring-lagoon/20";
}

export function secondaryButtonClassName() {
  return "rounded-2xl border border-shell-border/45 bg-shell-panel/80 px-4 py-2.5 text-sm font-medium text-ink transition hover:border-lagoon hover:text-lagoon";
}

export function primaryButtonClassName() {
  return "rounded-2xl bg-lagoon px-4 py-2.5 text-sm font-semibold text-white transition hover:bg-lagoon-hover disabled:cursor-not-allowed disabled:opacity-70";
}

export function clickablePanelClassName(extra = "") {
  return `cursor-pointer transition hover:ring-2 hover:ring-lagoon/30 hover:shadow-lagoon/10 ${extra}`.trim();
}

export function QuickViewDialog({
  open,
  onClose,
  title,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
}) {
  useEffect(() => {
    if (!open) return;
    function onKey(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div
        className="absolute inset-0 bg-black/50 backdrop-blur-sm"
        onClick={onClose}
        aria-hidden="true"
      />
      <div className="relative z-10 w-full max-w-lg glass-panel rounded-[1.75rem] p-6 shadow-aura">
        <div className="mb-5 flex items-start justify-between gap-4">
          <p className="font-display text-xl font-semibold text-ink">{title}</p>
          <button
            type="button"
            onClick={onClose}
            className="shrink-0 rounded-full p-1.5 text-shell-soft transition hover:bg-shell-panel/80 hover:text-ink"
            aria-label="Close"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}