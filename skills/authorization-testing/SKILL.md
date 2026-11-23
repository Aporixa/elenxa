---
name: authorization-testing
description: Use when testing object-level, function-level, role, tenant, ownership, or workflow authorization on an authorized application.
---

# Authorization testing

Build an actor-action-object matrix and test server-side decisions with controlled identity changes.

## Establish controls

Identify the actors available to the task, their roles or tenants, and objects each actor owns. Capture a valid request for an allowed action before modifying it.

For each hypothesis, vary only one boundary:

- object identifier or parent identifier;
- actor session while preserving the request;
- role, tenant, organization, or workspace context;
- HTTP method, alternate route, batch operation, or export path;
- workflow stage or approval state.

Compare the response and the resulting target state. A `200` with no unauthorized data or side effect is not sufficient proof, and a `403` on one route does not prove equivalent routes are protected.

## Coverage

Prioritize actions that read sensitive records, mutate ownership or permissions, perform bulk operations, export data, or cross tenant boundaries. Register actor and object relationships as facts so sibling workers can avoid repeating the same matrix cells.

Use `search_research_knowledge` when an unfamiliar framework or identifier scheme changes how authorization is normally enforced.

## Completion

Record the actor, expected permission, object, action, baseline, modified request, and independent read-back. Mark untested matrix cells explicitly. Report only server-observable access or effects as confirmed authorization findings.
