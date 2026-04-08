# Stateless Handoffs, Event Journals, and Self-Improvement

*Published: 2026-04-07*

---

go-orca's engine has no internal message queue, no shared in-memory state between personas, and no long-running agent process that accumulates context across phases. Each persona receives a `HandoffPacket`, produces a `PersonaOutput`, and stops. The engine merges the output, writes it to storage, and moves on.

This is not an accident of implementation. It is a design constraint with specific consequences — some of which turned out to be unexpectedly useful.

## The HandoffPacket is the only communication channel

When the Implementer finishes, it does not "tell" QA anything directly. The engine builds a new `HandoffPacket` from the current `WorkflowState` — which now includes the Implementer's artifacts — and passes that to QA. QA sees a snapshot of the world as it stood after the Implementer ran. It cannot ask the Implementer a question. It cannot request a clarification from the Director.

This is strict, but it has a useful property: every persona always operates on a consistent view of the workflow. There are no race conditions, no "I saw an artifact that was later removed," no partial writes. The snapshot is the truth.

The flip side is that you cannot use go-orca's built-in persona flow for inherently interactive workloads — where one AI agent needs to ask another agent a follow-up question mid-task. For that you need a different topology. go-orca handles batch orchestration: a request goes in, a workflow comes out.

## The event journal as the source of truth

Every state transition, every persona completion, and every significant engine action is written to the event journal via `journal.AppendEvents`. Events are immutable and append-only. The workflow's final state in the `workflows` table is a derivative — useful for fast retrieval, but the journal is the authoritative record.

This matters for a few operational reasons:

**Resume semantics.** When a workflow is paused and resumed via `POST /workflows/:id/resume`, the engine reads the workflow state (not replays the journal). But the journal is available for audit: you can see exactly which phases completed before the pause and what each one produced.

**Streaming.** `GET /workflows/:id/stream` is an SSE endpoint that tails the journal in real time. The frontend or calling system does not poll — it subscribes and receives events as they are written. The `EventsSince` store method lets callers catch up from a known timestamp.

**Debugging.** A failed workflow leaves its full journal intact. Every event that fired before the failure is queryable. The `refiner.suggestion` events — emitted after the Finalizer runs — are also in the journal, which means the quality signal from the inline refiner is part of the permanent record.

## Self-improvement through the inline refiner

The Finalizer persona includes an embedded refiner: a structured LLM call that evaluates the workflow's output against the original request and produces a `RefinerImprovement` list. These improvements are scored with a `health_score` (0–100) and surfaced as `refiner.suggestion` SSE events.

This is not a self-correction loop. The refiner does not trigger a re-run of any phase. It produces observations that the engine emits as events and stores in `WorkflowState.Finalization.RefinerImprovements`. The caller receives them and decides whether to act.

The useful part is that the refiner sees the same `HandoffPacket` the Finalizer saw — which includes all artifacts, all QA findings, all summaries, and the original request. Its evaluation is therefore grounded in the full context of the workflow, not just the final output.

Over time, if you collect `health_score` values across many workflow runs, you can build a picture of which request types, which customization setups, and which persona configurations produce consistently high-quality output. The journal gives you the data. What you do with it is up to you.

## Why stateless handoffs make this tractable

If personas maintained long-running state — open model contexts, accumulated tool call histories, partial outputs — the journal would be insufficient as an audit trail. You would need to replay the full conversation to understand what a persona did and why.

Stateless handoffs mean each persona's decision is bounded: it saw this `HandoffPacket` and produced this `PersonaOutput`. That's the complete record. The journal captures it. The snapshot model makes it auditable, resumable, and debuggable without requiring you to reconstruct any external state.

That is the core tradeoff go-orca makes: less flexibility in the interaction model, more tractability in everything that comes after.
