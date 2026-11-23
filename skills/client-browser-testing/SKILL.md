---
name: client-browser-testing
description: Use when testing browser trust boundaries, DOM behavior, cross-origin policy, storage, messaging, navigation, or client-side security on an authorized target.
---

# Client and browser testing

Separate browser-observable behavior from server behavior and prove the execution context that creates impact.

## Trace the data flow

Identify the source, transformation, sink, document origin, frame context, and security policy. Include URL inputs, DOM APIs, storage, service workers, `postMessage`, redirects, CSP, CORS, and browser rendering only when observed.

Use a harmless marker first. For execution hypotheses, capture the exact origin and sink and verify through a browser-observable effect. For cross-origin hypotheses, test both the preflight or request and whether the browser exposes the response to the initiating origin.

Use `search_research_knowledge` for unfamiliar browser policies. Use the browser tool when DOM or policy behavior is decisive; raw HTTP cannot establish every browser result.

## Evidence thresholds

- Reflection in HTML or JSON is not execution.
- A permissive CORS header is not impact without a browser-readable sensitive response.
- Client-side route guards are leads to server-side authorization tests.
- Storage content is not automatically secret unless an attacker-controlled context can reach it.
- `postMessage` behavior requires sender, receiver, origin checks, and message handling evidence.

## Completion

Record the source-to-sink path, origin and frame context, browser observation, relevant response headers, server-side dependency, and repeatability. State when browser automation or policy restrictions left the result inconclusive.
