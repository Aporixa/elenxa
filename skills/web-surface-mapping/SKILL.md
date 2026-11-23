---
name: web-surface-mapping
description: Use when mapping an authorized web application's reachable routes, parameters, technologies, trust boundaries, and distinct attack surfaces before deep testing.
---

# Web surface mapping

Build a coverage map before committing most of the task budget to one route.

## Map the surface

1. Verify the target address, scope, and network path with a bounded baseline request.
2. Record the application entry points, redirects, technologies, authentication states, and content types actually observed.
3. Enumerate distinct surfaces: server-rendered routes, APIs, static bundles, uploads, administrative paths, alternate hosts or ports, and state-changing workflows.
4. Add discovered services and endpoints as assets with their source. Reuse existing assets instead of registering duplicates.
5. Rank follow-up work by reachability, privilege boundary, data sensitivity, and independence from routes already being tested.

Use `api-recon` when the assigned objective is exhaustive frontend/API extraction. This skill produces a testing map, not a claim of complete API coverage.

## Coverage discipline

- Separate observed routes from guessed names.
- Sample error behavior and at least two meaningful application states when available, such as anonymous and authenticated.
- Treat robots files, source maps, comments, and client configuration as leads whose runtime effect still needs verification.
- After two equivalent misses, change the discovery method or record the path as tentatively absent.

## Completion

Finish with a list of mapped surfaces, the evidence behind each, untested surfaces, and the independent routes that deserve worker intents. A single successful route does not complete broad mapping.
