# Skill Improvement: QA Validation Remediation Limits

## Overview
This document updates the QA validation skill to explicitly define **remediation limits** that prevent infinite cycling when addressing the same blocking issue repeatedly. It establishes clear escalation criteria for when to move from "QA validation" to "QA exhausted" state.

## Problem Statement
The current QA validation skill excels at identifying blocking issues but lacks guidance on **when to escalate** versus **continue cycling**. Without explicit limits:
- Teams may spend weeks remediating the same issue in different ways
- No mechanism exists to declare "we've exhausted reasonable remediation attempts"
- Remediation loops can stall workflows indefinitely without clear exit criteria

## Proposed Fix: Remediation Limits Section

Add the following section to the **QA Validation Skill**:

### Remediation Limits

**Escalation Criteria — When to Declare "QA Exhausted":**

1. **Two-Iteration Failure Rule**
   - If the same blocking issue persists after **2 complete remediation cycles** without convergence, escalate.
   - Count cycles by distinct remediation attempts, not minor edits.

2. **Identical Root Cause Reappearance**
   - If the same root cause manifests again after a "fix" (e.g., adding context where it was previously missing), it indicates incomplete understanding rather than new blocking issues.
   - Escalate when the same pattern requires repeated fixes.

3. **Alternative Remediation Failure**
   - If 2 different approaches to the same issue both fail within the 2-cycle window, the issue likely exceeds current knowledge or requires architectural change.
   - Document the failed approaches and escalate.

4. **Cross-Team Impact**
   - If the blocking issue affects multiple teams or components without clear resolution path after 2 cycles, escalate with full context of failed attempts.

5. **Knowledge Gap Identification**
   - If repeated failures suggest missing knowledge (e.g., misunderstood Go idiom, unclear specification), escalate with documentation of the gap rather than continuing to cycle fixes.

**Cycle Definition:**
- A remediation cycle = identify → remediate → verify → report
- Each cycle must include verification that the issue is truly resolved (not just "fixed" temporarily)

**Escalation Deliverables:**
When escalating "QA exhausted":
- Document the 2+ failed remediation cycles
- Summarize why each attempt failed
- Propose alternative approaches or architectural changes needed
- Request review from architect or specification review

## Implementation Notes

### For QA Validators:
- Count cycles explicitly and log each one
- Before starting cycle 3 of the same issue, review cycles 1-2
- Distinguish between "new issue discovered" (continue) and "same issue reappears" (escalate)

### For Implementers:
- Accept that some issues require architectural review, not just code fixes
- Escalation is not failure; it's a valid workflow state
- Provide full context in escalation requests

### For Workflow Orchestrators:
- Track cycle counts per issue
- Automatically suggest escalation after 2 cycles
- Provide escalation templates and guidelines

## Example Scenarios

### Scenario A: Context Propagation (Should Escalate After 2 Cycles)
```
Cycle 1: Add context to function A
Cycle 1 verification: Function A passes but function B fails (same missing context)

Cycle 2: Add context to function B + all callers
Cycle 2 verification: Function B passes but function C fails

Cycle 3 would be escalation point because:
- Same root cause (missing context propagation)
- 3+ functions affected, suggesting pattern not addressed
```

### Scenario B: Package Matching (May Require New Cycle)
```
Cycle 1: Add package declaration to test file
Cycle 1 verification: Passes but reveals import error

Cycle 2: Fix import error
Cycle 2 verification: Now fails due to interface definition

Cycle 2 is valid continuation (different issue).
```

## Verification Checklist

When considering escalation:
- [ ] Counted 2+ complete remediation cycles for this issue
- [ ] Verified each cycle was distinct (not minor edits)
- [ ] Confirmed root cause is same (not different issues)
- [ ] Documented all failed remediation attempts
- [ ] Proposed alternative approaches or need for architectural change

## Impact

- **Prevents infinite loops** by defining clear exit criteria
- **Saves team time** on issues that require architectural review
- **Improves decision quality** through escalation reviews
- **Encourages learning** by surfacing knowledge gaps early
- **Reduces burnout** from "whack-a-mole" fix cycles

---

*Document generated for skill self-improvement workflow.*