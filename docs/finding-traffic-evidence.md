# Multi-traffic evidence for findings

"Linked traffic" on the finding detail page supports cross-page multi-select, purpose and notes, ordering and unbinding. The traffic page can also multi-select records and link them to one existing finding in one go. Finding evidence inherited by a task is read-only; to change it, go to the source task.

"Agent auto-bind traffic" in System Settings is off by default; it is read and changed through `agent_traffic_binding` on `/api/settings`. Turning it on increases token usage from reviewing requests/responses, tool calls and prompts; the next Agent round uses the new setting. When off, the auto-bind parameters and the backfill-binding tool are hidden, no auto-bind guidance is injected, and new auto-bind submissions from already-running sessions are rejected. Manual binding, traffic capture, reading saved evidence and export are unaffected.

When on, the default flow is "finding recorded → report Agent triggered automatically → traffic verified and bound → report written from the latest evidence version". The reporter keeps the verification command, key output, any real traffic IDs already known and their purpose in `evidence`; the report Agent combines the finding details and execution record, verifies with `traffic_search` / `traffic_get`, then calls `bind_finding_traffic`, and reads the latest `version` before saving the report. When the switch is off, the report Agent gets no extra raw traffic search/read tools and does not bind automatically; it can still read manually bound snapshots and generate the report.

Compatible with existing callers: `traffic_refs` / `evidence_hint_id` on `report_finding` can still bind explicitly and immediately. Binding is optional: for non-HTTP findings such as TCP, or when nothing was captured or no exact record can be found, omitting references still allows normal reporting and report writing; keep other verifiable evidence such as command output and logs, and preferably state why nothing was bound. No new required fields are added. Submitted IDs must all be valid with complete bodies. Any single failure rolls back the whole binding operation; if an explicit bind submitted with a report fails, the whole report is rolled back. Appending the same snapshot again adds no binding and does not overwrite its note.

## Agent IDs and report versions

`report_finding` gains an optional parameter; the array order is the initial evidence order:

```json
{
  "traffic_refs": [
    {"traffic_id": "real traffic ID", "role": "baseline", "note": "normal account request"},
    {"traffic_id": "another real traffic ID", "role": "proof", "note": "reproduction request"}
  ]
}
```

Purposes are `baseline` (normal comparison), `proof` (proof of the vulnerability), `verification` (additional verification) and `supporting` (supporting evidence, the default). First check the real records with `traffic_search` / `traffic_get`; domain and time are only for filtering candidates and do not imply task attribution.

The prompts follow [CyberStrikeAI's vulnerability reporting tool guidance](https://github.com/RuoJi6/CyberStrikeAI/blob/54d56774b8bd285817d16d48b70a4a5e6e0963f7/internal/app/vulnerability_tools.go), combined with this project's optional-binding convention, without adding a required "reason for no packet" check. IDs must not be guessed, and targets should not be re-probed just to obtain packets.

The first line of the response is still `finding recorded: <exploration node ID>`; the JSON that follows gives the standalone finding record `finding_id`, the exploration node `finding_node_id` and a binding summary.

- `get_finding_traffic(finding_id)` takes the **standalone finding record ID** and returns the ordered list and `version`; pass `binding_id`, `side=request|response`, `offset` and `length` to read in segments of at most 8192 bytes each.
- `finding_id` in `update_finding_report` **still takes the exploration node ID**. The new `evidence_version` is the version actually read; if the evidence changes during generation, writes against the old version are rejected and the report must be re-read and regenerated.
- When an old-style report call passes no version, it does not claim to cover existing traffic evidence. After a change to bindings, notes, purposes or order, the existing report is flagged as needing an update.

With auto-bind on, `add_hint` / `add_task_hint` can save `traffic_refs` on a single hint or on each element of a batch `hints`. When the Planner reports on someone's behalf it can pass `evidence_hint_id` to explicitly select the references in this task's matching hint; inherited hints cannot be referenced. The system never guesses bindings from domains, times or browsing history. A failed submission creates no partial finding and does not trigger the report early.

When an existing finding is missing bindings, use `bind_finding_traffic(finding_id, traffic_refs)` to backfill them without re-recording. `list_findings` / `list_task_findings` / `node_detail` / `get_task_node_detail` return explicit `finding_id` and `finding_node_id`; the old `id` keeps its exploration-node meaning.

On startup, only optional properties are added to old tool schemas; the original default traffic tool bindings are extended to the report Agent, and the backfill-binding tool goes to the report Agent by default. Custom binding lists, prompts, descriptions and enabled states are preserved. Guidance is added once after final tool assembly: reporting roles hand off existing evidence, and the report Agent verifies, binds and writes the report. When a platform conversation lacks task context, it should hand off to the task Agent through a structured hint rather than reporting directly. Existing evidence should be handed off before the task is judged complete; with no packets there is no forced wait. A failed `report_finding` does not trigger the report Agent.

## API

Base path: `/api/exploration/findings/{finding_id}/traffic`, using the standalone finding ID. Existing authentication applies; `context_task` checks task visibility and inherited read-only status.

| Method / relative path | Request / response |
| --- | --- |
| `GET` | Ordered summary, evidence version, version used by the report |
| `POST` | `{"traffic_refs":[...]}` appended as a batch |
| `PATCH /{binding_id}` | `{"version":1,"role":"proof","note":"note"}` |
| `DELETE /{binding_id}` | `{"version":1}` |
| `PUT /order` | `{"version":1,"binding_ids":["2","1"]}`, must be the complete list |
| `GET /{binding_id}` | Snapshot metadata and a bounded body preview |
| `GET /{binding_id}/body` | `side`, `offset`, `length`; `download=1` downloads the complete original bytes |

Version/order set conflicts and writes during archiving return `409`; writes to inherited evidence return `403`; bindings that do not exist or do not belong to the finding return `404`; traffic/attachment read and verification failures return explicit errors.

## Storage and migration

Idempotent PostgreSQL migration on startup: adds `traffic_evidence_snapshots`, `finding_traffic_bindings`, and `findings.evidence_version` / `report_evidence_version` (default 0). No bindings are backfilled by guessing from historical text.

A snapshot stores the original traffic ID, capture time, URL, method, status, request/response headers, body length and SHA-256. Bodies are stored by hash at `<data>/evidence/blobs/<first two chars>/<hash>.bin`, separate from the cleanable `data/traffic`; multiple findings can share snapshots/bodies. Snapshots have no content-update API; on a verification mismatch, reads and exports fail.

The complete body is read under the original traffic write lock, including large body blobs and old directory records. Files are persisted and verified first, then the exploration node, intent relation, finding, snapshot and binding are written in a single PostgreSQL transaction; the planner is notified only after commit. A failure may leave unreferenced files but never produces partial business records.

PostgreSQL advisory lock `7337741004` coordinates evidence files and SQL references; the task row lock forbids evidence changes once archiving is queued. Restore holds the evidence lock from body installation through metadata commit. Deleting a finding cascades to its bindings.

The cleaner runs hourly and reclaims only unreferenced content not involved in an in-progress operation, with a delay of at least 24 hours. Ordinary traffic cleanup does not touch the evidence directory. When backing up hot data, back up PostgreSQL and `data/evidence` together.

## Export and archiving

Markdown includes the ordered evidence list and version; JSON includes metadata; CSV adds the count and binding IDs. `md-zip` keeps the finding Markdown and provides:

```text
evidence/<finding_id>/<binding_id>/
  manifest.json
  request.http
  response.http
  request.bin
  response.bin
```

The Markdown references the messages through relative links. Before the download is sent, attachment copying, hash verification, compression, disk sync and a CRC read check of every ZIP entry are completed; anything missing or corrupt fails the whole download. Complete attachments keep the original binary bytes.

Archive v3 collects snapshots and bodies by finding binding, without depending on the original traffic or domains. Hot data is cleaned only after the package is verified; shared evidence is kept. Restore verifies and installs bodies first, then restores metadata and bindings transactionally, and supports retry on failure. v1/v2 remain restorable, with missing new fields explicitly set to 0.

## Verification and boundaries

Give each test package its own freshly created PostgreSQL test database, specified through `ELENXA_PG_DSN`, so leftover task/model fixtures do not trigger background runs. Run the full tests of the relevant packages and confirm there are no skips caused by missing configuration:

```sh
# Before each package, set ELENXA_PG_DSN to that package's own test database; an explicit configuration failure must error.
go test ./<package> -count=1
go test -race -p 1 ./evidence ./db ./agent ./server -run 'TestEvidence|TestFindingTraffic|TestFindingEvidence|TestReportFindingAtomicContract|TestTaskArchive'
```

Frontend verification includes `npx tsc --noEmit`, a Biome check of the affected files, a Webpack build and a `NEXT_EXPORT=1` static export. Use a separate cache directory to avoid overwriting a running dev server.

Local end-to-end acceptance uses a separate port, controlled HTTP / domain HTTPS targets and a temporary data directory, covering both binding entry points, cross-page selection, ordering/notes, error messages, inherited read-only status, downloads, and, after deleting the original traffic: export, archiving, reclaiming hot bodies, restore and hash verification.

The first version uses a global evidence coordination lock; during large binding/export batches or slow attachment downloads, other evidence operations may wait. Evidence cannot be fabricated when the recording or complete body is missing. This feature does not change the capture switch and does not handle certificate issues for HTTPS accessed directly by IP.
