import type { ScopeKind, WorkflowMode } from "../../types/orca";

export const workflowModes: Array<{ value: WorkflowMode; label: string }> = [
  { value: "software", label: "Software" },
  { value: "content", label: "Content" },
  { value: "docs", label: "Docs" },
  { value: "research", label: "Research" },
  { value: "ops", label: "Ops" },
  { value: "mixed", label: "Mixed" },
];

export const deliveryActions: Array<{ value: string; label: string; description: string }> = [
  { value: "", label: "None (default)", description: "No delivery action — artifacts returned inline." },
  { value: "api-response", label: "API Response", description: "Return all artifacts inline in the API response." },
  { value: "github-pr", label: "GitHub PR", description: "Open a GitHub pull request with all artifacts." },
  { value: "repo-commit-only", label: "Repo Commit", description: "Commit artifacts directly to a branch without a PR." },
  { value: "create-repo", label: "Create Repo", description: "Create a new GitHub repo and seed it with artifacts." },
  { value: "artifact-bundle", label: "Artifact Bundle", description: "Package artifacts into a downloadable bundle." },
  { value: "markdown-export", label: "Markdown Export", description: "Export all artifacts as a single markdown document." },
  { value: "blog-draft", label: "Blog Draft", description: "Produce a publication-ready blog post draft." },
  { value: "doc-draft", label: "Doc Draft", description: "Produce a final polished document draft." },
  { value: "webhook-dispatch", label: "Webhook", description: "POST artifacts and metadata to a webhook URL." },
];

export const scopeKinds: Array<{ value: ScopeKind; label: string }> = [
  { value: "global", label: "Global" },
  { value: "org", label: "Org" },
  { value: "team", label: "Team" },
];

export function formatDate(value?: string | null) {
  if (!value) {
    return "-";
  }

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }

  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

export function formatRelative(value?: string | null) {
  if (!value) {
    return "-";
  }

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }

  const deltaMs = date.getTime() - Date.now();
  const formatter = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  const minutes = Math.round(deltaMs / 60_000);

  if (Math.abs(minutes) < 60) {
    return formatter.format(minutes, "minute");
  }

  const hours = Math.round(minutes / 60);
  if (Math.abs(hours) < 24) {
    return formatter.format(hours, "hour");
  }

  return formatter.format(Math.round(hours / 24), "day");
}

export function toneForStatus(status?: string) {
  switch (status) {
    case "completed":
    case "ready":
      return "bg-shell-success/15 text-shell-success-text";
    case "running":
    case "pending":
    case "paused":
      return "bg-shell-warning/18 text-shell-warning-text";
    case "failed":
    case "cancelled":
      return "bg-shell-danger/16 text-shell-danger-text";
    default:
      return "bg-shell-border/10 text-shell-muted";
  }
}

export function prettyJson(value: unknown) {
  return JSON.stringify(value, null, 2);
}