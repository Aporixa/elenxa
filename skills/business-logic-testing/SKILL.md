---
name: business-logic-testing
description: Use when testing workflow order, limits, pricing, inventory, credits, approvals, race conditions, or domain invariants on an authorized application.
---

# Business-logic testing

Model the workflow's invariants and test whether the server preserves them across sequence, repetition, and concurrency.

## Build the workflow model

List the states, transitions, actors, resources, counters, and expected invariants. Examples include one-time use, non-negative balances, ownership preservation, approval before execution, and consistent totals across representations.

For each test:

1. Establish the normal sequence and read back its final state.
2. State the invariant and one way the sequence might violate it.
3. Change one dimension: order, repetition, quantity, actor, stale token, or controlled concurrency.
4. Read back every affected resource, not only the immediate response.
5. Reset or account for retained state before the next independent trial.

Keep concurrency bounded by the task traffic policy. A race claim requires repeated evidence that distinguishes concurrency from ordinary eventual consistency or a flaky response.

Use `search_research_knowledge` when a framework mechanism matters, but derive the invariant from the target's observed domain behavior.

## Completion

Report the expected invariant, altered sequence, number and timing of requests, resulting durable state, reproducibility, and cleanup. Preserve failed sequences because they define covered workflow branches.
