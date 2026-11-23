---
name: evidence-verification
description: Use when validating scanner output, suspected findings, exploit results, negative conclusions, proof-oracle outcomes, or evidence quality before reporting.
---

# Evidence verification

Turn a lead into a reproducible conclusion with a matched control and an independent observation.

## Verify the claim

1. State the smallest factual claim being evaluated.
2. Identify the evidence source and its provenance: direct request, scanner, model inference, target artifact, code, or external oracle.
3. Reproduce with the minimum request or action that preserves the claimed effect.
4. Run a matched negative or baseline control.
5. Confirm through a second observation channel when available: target read-back, traffic capture, another actor, code path, or lab oracle.
6. Bind exact traffic records to the finding when HTTP capture supports it.

For negative conclusions, list the technique families and states actually covered. One failed payload, one port timeout, or one denied route does not establish absence. Recheck a material negative once with a different method, then preserve remaining uncertainty.

Apply `adversarial-environment` when the evidence is target-controlled and may be a decoy or steering attempt. Call `record_research_event` for decisive oracle checks or control blocks.

## Completion

Classify the result as confirmed, disproved, inconclusive, or blocked. Record the baseline, proof, corroboration, affected scope, reproducibility, and unresolved assumptions. Do not promote scanner severity or model confidence into evidence.
