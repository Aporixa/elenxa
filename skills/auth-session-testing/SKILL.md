---
name: auth-session-testing
description: Use when testing authentication, sessions, tokens, JWTs, password reset, account recovery, login state, or account lifecycle behavior on an authorized target.
---

# Authentication and session testing

Model the full account state machine, then test transitions and token boundaries rather than isolated login payloads.

## Baseline

Capture one valid and one invalid flow when the task provides suitable accounts. Record cookies, tokens, redirects, status codes, and server-side state changes without exposing secrets beyond the task evidence.

Map:

- account creation, verification, login, logout, recovery, and credential change;
- session creation, renewal, revocation, expiry, and concurrent-session behavior;
- token issuer, audience, subject, algorithm, claims, transport, and storage;
- transitions that should require recent authentication or a second factor.

## Test hypotheses

Choose hypotheses supported by the observed design. Use `search_research_knowledge` when the token format or authentication mechanism is unfamiliar.

- Can a transition be skipped, replayed, reordered, or completed with stale state?
- Does logout or credential change invalidate every relevant session?
- Are account identifiers, recovery tokens, or verification codes bound to the correct user and action?
- For JWTs, does verification enforce the expected algorithm, key, issuer, audience, time claims, and token type?
- Do anonymous, partially authenticated, and fully authenticated requests reach different server-side checks?

Change one material variable at a time and compare against the baseline. Rate-sensitive login or recovery tests must respect the task's traffic policy.

## Completion

Report the exact state transition tested, control request, modified request, observable outcome, and whether the result was independently reproduced. Do not infer a token weakness from decoding alone or an account takeover from a client-side response alone.
