# Why QA Can't Write Code (and Why That's a Feature)

*Published: 2026-04-04*

---

One of the first questions people ask when they see go-orca's persona model is: why can't the QA persona fix the bugs it finds? Surely it would be more efficient to let QA patch the code rather than just filing findings?

It would be more efficient in a narrow sense. It would also undermine the only safety property the engine actually has.

## Roles are enforced at the data layer, not the prompt layer

Every task in a go-orca workflow has an assigned persona kind: `Director`, `Project Manager`, `Architect`, `Implementer`, or `QA`. The assignment is made by the Architect persona during task planning and stored in `WorkflowState.Tasks`.

When a persona completes its run, the engine merges its output back into the workflow state. But the merge is filtered. In `engine.go`, `applyOutput` checks every outgoing artifact and every proposed task mutation against the persona's assigned role. If QA returns an artifact with a source file path that was not assigned to QA, it is silently discarded. If QA attempts to add a new task, it is discarded.

This is not done through prompt engineering. "You are a QA persona, please do not write code" is not something you can enforce with a system prompt — the model can always reason its way around it, especially if the context makes code generation feel like the obviously helpful thing to do. The enforcement is in Go: the output schema for QA does not include a code artifact write path, and the engine's merge step validates assignments before accepting any state change.

## What QA can actually do

QA's job is to validate artifacts produced by the Implementer and write findings. A finding has a severity, a description, a reference to the artifact being challenged, and optionally a suggested fix as a plain text note. The Finalizer persona reads these findings when composing the final output.

The Implementer persona, in a subsequent iteration if one is configured, could theoretically act on QA findings. But in a single-pass workflow — which is the default — QA findings go into the handoff packet and end up in the Finalization result. The caller sees them. The caller decides what to do next.

This is intentional. The engine is not a closed loop that retries until quality thresholds are met. It is a single-pass orchestrator that gives the caller visibility into what happened and leaves decisions about follow-up to the system that invoked it.

## The alternative and why we avoided it

The alternative is a self-correcting agent loop: QA finds a bug, triggers a re-run of the Implementer, Implementer fixes it, QA re-validates, and so on until some exit condition is satisfied. This is a reasonable pattern for certain workloads.

It is also a pattern where the blast radius of a bad LLM output grows with each iteration, context windows fill up with correction attempts, and it becomes very hard to reason about what the final output actually represents.

go-orca makes a deliberate choice to be a single-pass, observable system. Every phase is journaled. Every artifact has a clear provenance. The caller can see exactly what each persona contributed. That traceability only holds when roles are fixed — when QA writes code, the audit trail for that code points to QA, which is confusing, and the role boundary that makes the audit trail meaningful collapses.

## The practical implication

If you want a workflow that iterates until QA passes, build that loop outside the engine. POST a workflow, check the finalization result's QA findings, and POST a follow-up workflow with the findings as additional context. The engine gives you the primitives; orchestrating retries is a caller concern.

This keeps the engine simple and its behavior predictable. Predictability is a feature, not a limitation.
