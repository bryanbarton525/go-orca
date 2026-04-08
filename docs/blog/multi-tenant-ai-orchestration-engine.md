# Multi-tenant AI Orchestration: What it Actually Means

*Published: 2026-04-01*

---

The phrase "multi-tenant AI orchestration engine" gets used a lot. Most of the time it means one of two things: either someone has wired a shared LLM API key behind a thin auth layer, or they have taken a single-tenant agent framework and bolted on a per-user prefix. Neither is orchestration. Neither is multi-tenancy.

Here is what we mean when we use that phrase for go-orca.

## Multi-tenancy is a trust boundary, not a feature flag

In go-orca, every piece of persistent state — workflows, events, scopes, artifacts — carries a `tenant_id`. That ID is resolved from a JWT at the HTTP middleware layer before any handler runs. It is never accepted from the request body. Handlers that read or mutate workflows first call `checkWorkflowOwnership`, which fetches the workflow and returns 403 if `ws.TenantID` does not match the resolved tenant. There is no "admin bypass" for cross-tenant reads in the production path.

Scopes are a second dimension. A tenant may have many scopes — one per team, environment, or product line. Scopes carry their own customization trees and their own hierarchy. A `senior-dev.agent.md` file with `scope_slug: "engineering"` is invisible to the `"content"` scope. Isolation is structural, not conditional.

This is what multi-tenancy means: data belonging to one tenant cannot be accessed, modified, or even observed by another tenant through any normal code path, without a deliberate architectural decision to allow it.

## Orchestration is sequencing with policy, not chaining with strings

Go-orca runs a fixed sequence of AI personas: Director → Project Manager → Architect → Implementer → QA → Finalizer. Each persona operates on a `HandoffPacket` — a snapshot of the workflow state passed forward, never backward.

This is not "chaining prompts." The Director scopes the request. The Project Manager defines requirements. The Architect produces a task graph. The Implementer executes against it. The QA persona validates output and can produce findings that are visible to the Finalizer, but QA cannot retroactively modify the Implementer's artifacts — the hand has already been played. Orchestration means the engine controls sequencing and policy; no individual persona can subvert the flow.

Role enforcement is implemented in the engine, not in prompts. If a QA persona output attempts to inject new tasks into `ws.Tasks`, the engine discards them. If it attempts to modify an artifact not assigned to QA, the engine discards that too. The rules are in Go, not in the system prompt.

## What this makes possible

Because tenants are isolated and the persona sequence is policy-enforced, go-orca can safely run workflows for different tenants concurrently without risk of cross-contamination. The customization system means each tenant can have radically different personas, skills, and delivery actions — without any of that bleeding into another tenant's workflows.

A workflow that opens a GitHub PR for tenant A cannot open a PR for tenant B. A customization file loaded for scope `"engineering"` is not visible to scope `"content"`. These are not policies you configure — they are invariants of the data model and the engine.

That is what "multi-tenant AI orchestration engine" means.
