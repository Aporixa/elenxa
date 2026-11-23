---
name: injection-testing
description: Use when testing whether authorized input crosses SQL, command, template, expression, header, XML, serialization, or other interpreter boundaries.
---

# Injection and parser-boundary testing

Identify the likely interpreter and prove that controlled input changes its behavior. Avoid unbounded payload spraying.

## Experiment loop

1. Capture a stable baseline request and response.
2. Locate the input's syntactic context from errors, transformations, content type, code, or paired behavior.
3. State one interpreter hypothesis.
4. Use the smallest non-destructive probe that distinguishes data from syntax.
5. Compare timing, output, error, and server-side effect against a matched control.
6. Change encoding, transport, or syntax only when the observation supports that change.

Use `search_research_knowledge` for an unfamiliar parser or filter behavior. Prefer a small relevant technique slice over a generic payload list.

## Evidence thresholds

- Reflection alone is not script execution.
- An error alone is not injection.
- A timing result needs repeated controls and a meaningful separation.
- A scanner label needs the underlying request and response verified.
- A successful parse bypass still needs an observable security consequence or trust-boundary violation.

Stop repeating equivalent payloads after two results with the same transformation and response class. Record the transformation, then test a different grammar, transport, or hypothesis.

## Completion

Preserve the baseline, probe, control, observations, interpreter hypothesis, and confidence. State which technique families remain untested before calling the input non-injectable.
