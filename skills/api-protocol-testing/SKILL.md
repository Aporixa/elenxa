---
name: api-protocol-testing
description: Use when testing REST, GraphQL, WebSocket, server-sent event, RPC, batch, or versioned API behavior on an authorized target.
---

# API and protocol testing

Model the protocol state and server-side contract before mutating requests.

## Establish the contract

Record transport, endpoint, method or message type, authentication, identifiers, content type, request shape, response shape, and state transition. For stateful protocols, record handshake data, subscription or channel identifiers, reconnect behavior, and message ordering.

Test hypotheses supported by that contract:

- undocumented methods, fields, versions, or batch operations;
- inconsistent authentication or authorization between equivalent transports;
- mass assignment, over-posting, aliasing, batching, or excessive query depth;
- WebSocket origin, channel, message, and reconnect authorization;
- event-stream leakage, stale subscriptions, or cursor misuse;
- parser differences caused by content type, duplicate keys, or encoding.

Use `api-recon` first when the assigned work is endpoint collection. Use `search_research_knowledge` when protocol-specific semantics are unfamiliar.

## Completion

Retain exact request or frame ordering and an independently observed result. A schema entry or accepted frame is a lead; confirm unauthorized data, state change, resource effect, or oracle result before reporting success.
