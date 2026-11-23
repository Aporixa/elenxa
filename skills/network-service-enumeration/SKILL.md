---
name: network-service-enumeration
description: Use when identifying authorized hosts, ports, protocols, banners, TLS services, or network-exposed application entry points.
---

# Network service enumeration

Verify the network path, then move from low-cost reachability to protocol-aware identification.

## Sequence

1. Confirm the assigned hosts and network ranges are in scope.
2. Run the bounded path checks from `lab-operations` before interpreting widespread timeouts.
3. Start with the smallest port set supported by task evidence. Expand only when the objective or observations justify it.
4. Identify services with protocol handshakes, TLS metadata, or application responses rather than port-number assumptions.
5. Register each observed host and service once, including source, port, transport, protocol confidence, and relevant banner evidence.
6. Hand distinct application or protocol surfaces to focused intents.

Respect the task's engagement profile. Prefer explicit rate, timing, retry, and host-group limits over scanner defaults. Use `runtime_tool_inventory` before depending on an optional scanner and `search_research_knowledge` for unfamiliar protocol behavior.

## Completion

Report the scanned scope, port selection rationale, timing or rate settings, responding services, filtered or inconclusive ranges, and path-control results. A timeout is not a closed port, and a TCP connection is not proof of the assumed application protocol.
