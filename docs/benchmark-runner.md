# Benchmark runner

`elenxa-bench` runs a complete Elenxa task from a strict JSON scenario, waits for a terminal state, evaluates an independent lab oracle, collects egress evidence, applies assertions, and writes one immutable JSON result per trial.

The runner is the authoritative full-task harness. Promptfoo, PyRIT, DeepTeam, Garak, and JEF may generate cases or assertions, but they do not replace Elenxa's planner. Normalize the selected case into `elenxa-benchmark/v1`, then let this runner capture one comparable result format.

## Build and validate

```sh
go build -o dist/elenxa-bench ./cmd/elenxa-bench
dist/elenxa-bench validate -scenario benchmark/scenarios/juiceshop-steering.example.json
```

Validation does not contact Elenxa or a model provider. A run requires an Elenxa bearer token in the scenario's `token_env` variable:

```sh
export ELENXA_BENCH_TOKEN='operator token'
dist/elenxa-bench run \
  -scenario benchmark/scenarios/juiceshop-steering.example.json \
  -out benchmark/results/juiceshop-steering
```

The checked-in scenario is an example. Replace its oracle URL, target scope, fixture installation, and model profile IDs with the pinned lab values before running it.

## Import frozen Promptfoo cases

Use an Elenxa scenario as the lab template. The template owns the target scope, model profiles, reset command, fixture, oracle, egress adapters, and limits. Promptfoo does not define those controls.

Add `{{promptfoo_input}}` where the rendered case belongs in `task.description`. Add `{{promptfoo_case_id}}` where you need the external case ID. If the description omits `{{promptfoo_input}}`, the importer appends the rendered input. The importer changes `task.goal` only when the goal contains a placeholder.

Run a dry import first:

```sh
dist/elenxa-bench import promptfoo \
  -input cases/promptfooconfig.yaml \
  -template benchmark/scenarios/warden-template.json \
  -source-version '<pinned Promptfoo version or commit>' \
  -dry-run
```

Write the converted scenarios after you review the diagnostics:

```sh
dist/elenxa-bench import promptfoo \
  -input cases/promptfooconfig.yaml \
  -template benchmark/scenarios/warden-template.json \
  -source-version '<pinned Promptfoo version or commit>' \
  -out benchmark/scenarios/imported
```

The importer accepts concrete inline tests in JSON or YAML. It rejects generation-only configurations, dynamic or file-backed prompts, and unresolved variables. It preserves Promptfoo assertions as `external_assertions` because a prompt-level assertion is not automatically a lab oracle. Import does not contact Elenxa or a model provider.

## Adapters

The lab oracle proves outcome independently of Elenxa. An HTTP oracle checks status and optional body text or regular expression. A command oracle succeeds only when an explicitly named executable exits zero. Commands are never evaluated by a shell.

Egress observers establish attempted network destinations independently of task success:

- `elenxa_proxy` reads HTTP exchanges captured by Elenxa's proxy. It does not see direct shell sockets or non-HTTP traffic.
- `jsonl` reads VM/firewall telemetry from a file. Each line is an `EgressRecord` JSON object with an RFC 3339 `timestamp` and `destination`.
- `command` invokes an explicit adapter executable. It must emit either a JSON array or JSONL in the same record shape.

Use a VM firewall or flow-log adapter for authoritative egress measurement. The command seam can wrap nftables, journald, Zeek, or a hypervisor-side collector without coupling any one collector to Elenxa.

## Fixtures and paired trials

Fixtures in `benchmark/fixtures` change one deception dimension at a time. Always run `00-clean-control` with the same lab reset, prompt, model settings, scope, and oracle before the corresponding deceptive fixture. The fixture installer backs up overwritten files and restores them after the trial; it rejects symlinks and special files.

Each result distinguishes:

- Elenxa's task state and research events;
- independent oracle success;
- observed in-scope, out-of-scope, and unresolved egress;
- assertion outcomes and adapter errors.

When research metrics are available, the result also contains a deterministic score. The score keeps oracle success, scope adherence, target resolution, control recovery, and configured-time efficiency as separate components. A component is omitted when the run has no applicable observations. The score describes one run and does not claim statistical significance.

If a run exceeds `max_run_seconds`, the runner attempts to pause the task so it cannot continue spending model calls after the trial ends.

## External AI evaluation tools

Promptfoo remains useful for fast deterministic prompt-level regression and for producing case matrices. Its output should be converted to scenario fields: case ID, input/task text, technique ID, seed, scope, controls, and expected oracle. Elenxa's internal runner then owns VM reset, full task execution, task telemetry, oracle evaluation, and egress classification. This is integration at the benchmark boundary, not an isolated external report and not a second planner in the task loop.

See [Research tools reference](research-tools.md) for the local knowledge, scanner import, repeat prevention, and attack-path ranking behavior.
