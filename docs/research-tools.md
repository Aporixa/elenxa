# Research tools reference

Elenxa provides deterministic services that support planner and worker decisions without another model-backed agent. These services make no network calls.

## Search the research catalog

`search_research_knowledge` searches the embedded `elenxa-knowledge/v1` catalog. The planner and workers can call the tool.

Inputs:

- `query` is required.
- `max_results` accepts 1 through 5. The default is 3.
- `max_chars` accepts 500 through 8,000. The default is 4,000.

The result includes the corpus version, SHA-256 digest, record IDs, procedures, corroboration checks, and sources. Retrieval uses local lexical matching. It does not use embeddings or a model.

The first catalog version covers control-path checks, indirect prompt injection, decoy credentials, false success, conflicting instructions, and AI-target benchmark separation. Add new records as versioned data. Do not add a large payload collection to an agent prompt.

## Import scanner evidence

`import_scanner_results` reads a relative file under the current task working directory. The planner and workers can import these formats:

- `sarif`
- `nmap-xml`
- `nuclei-jsonl`

The tool rejects absolute paths, paths that escape through a symlink, non-regular files, inputs larger than 32 MiB, and imports with more than 500 observations. Each observation becomes a fact with its source format, file name, SHA-256 digest, scanner identity, target, severity, and evidence. Imported observations are not confirmed vulnerability findings.

Example tool input:

```json
{
  "format": "nmap-xml",
  "path": "artifacts/initial-scan.xml"
}
```

## Prevent repeated calls

The final tool middleware fingerprints the tool name and canonical JSON arguments for each agent run. The first call executes normally. A second consecutive identical call executes and adds a warning. A third consecutive identical call returns a tool error without executing the tool.

Any different tool or argument set resets the sequence. The state does not persist between runs. The middleware covers built-in, custom, Model Context Protocol (MCP), skill, and domain tools.

## Rank attack paths

`rank_attack_paths` ranks open intents without changing their state or database priority. The planner can request up to 20 candidates. `graph_overview` includes the top five candidates.

The score uses these stored fields:

- intent priority;
- observed, inferred, and finding parent evidence;
- an asset scope anchor;
- an explicit `estimated_cost` value of `low`, `medium`, or `high`;
- prior blocked or exhausted intents with the same normalized summary.

The response includes every score component. The ranker does not infer semantic duplicates. It does not estimate cost from free-form text.

## Record provider controls

Norma normalizes Anthropic, OpenAI Chat Completions, and OpenAI Responses refusals to the same terminal reason. A refusal ends the current model loop without a retry. Planner refusal pauses the task. Worker refusal blocks the intent with `model_refused`.

Elenxa writes provider refusals as host control events. The research metrics endpoint includes these events even though the refused model cannot call `record_research_event`.
