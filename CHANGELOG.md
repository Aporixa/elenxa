# Changelog

Notable changes to this project are recorded in this file; the format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed

- **Renamed to Elenxa**, an Aporixa project. Go module path is now `github.com/Aporixa/elenxa`. All identifiers renamed: `ARTEX_*` env vars are now `ELENXA_*` (update your `.env`), binary `elenxa`, `cmd/elenxa`, `cmd/elenxa-browser-mcp`, compose project/service/volumes `elenxa`, Postgres user and database default `elenxa`, cookie `elenxa_token`, image `elenxa:local`, guard message "[Elenxa Platform Governance · ...]". Login username is `Elenxa`. Existing deployments need fresh volumes (new compose project name) or a manual migration.
- **Logo**: the ARTEX logo, favicon and screenshots are removed; the UI uses the Aporixa open-ouroboros mark (`BrandMark`, `app/icon.svg`).
- **Login notice**: the original author's terms of use dialog is replaced by a short responsible-use notice (authorized targets only, AGPL-3.0, no warranty). The author's extra usage restrictions are dropped (AGPL-3.0 §7).
- **README** rewritten: origin and attribution, AGPL-3.0 license section; the upstream community/QR section is removed.

### Added

- **SECURITY.md**: authorized-targets policy, recommended operating setup, private vulnerability reporting.
- **third_party/norma/LICENSE** (Apache-2.0) and **CHANGES.md** listing every file changed from Norma v0.4.3.

- **Prompt caching toggle** (settings `prompt_cache`, default on; switch on Function → LLM Records; `GET/PUT /api/settings`). With caching on, the Anthropic provider caches the whole system prompt even when no MCP cache boundary is set, and sends top-level automatic `cache_control` so conversation history is cached turn to turn. With it off, no `cache_control` is sent at all (the pre-patch request shape). The state is logged at startup and on change (`[engine] prompt caching: ...`). Previously, removing the outside MCPs left the cache boundary at 0, so no request was ever cached.

## [0.3.15-hardened-local] - 2026-11-17

Self-owned hardened local build (forked from 0.3.15).

### Security / hardening

- **Localhost only**: docker compose publishes 8787/8788 on `127.0.0.1`; PostgreSQL is no longer published to the host. `next dev`/`next start` bind to `127.0.0.1`, `allowedDevOrigins` restricted to localhost/127.0.0.1.
- **No self-update**: removed the in-app updater UI (update badge, update card, `/update/*` API client), `update.sh`, and the exit-code-75 restart loop in `start.sh`/`start.bat` (`start.sh` now `exec`s the binary so SIGTERM reaches it directly).
- **First-run setup token (no race to admin)**: `POST /api/auth/init` requires a one-time 64-hex token, printed in the server log and written (0600) to `setup-token` in the key directory; it is deleted once the admin password is set.
- **Key directory**: `jwt.key` and `setup-token` live in `ARTEX_KEY_DIR` (default: next to the binary; Docker: the private `artex-keys` volume at `/app/keys`, mode 0700), never in the browsable data dir.
- **Auth**: default bind `127.0.0.1:8787`; Host-header allowlist (`ARTEX_ALLOWED_HOSTS`) against DNS rebinding; CORS removed; cookie / `?token=` auth only on GET/HEAD; 12 h HS256 JWTs, signing key rotated on password change; login/setup rate limit (5 failures / 5 min per IP, 30 global).
- **Agents**: Norma file tools confined to the agent's working dir plus the skills dir (workers also see their task dir); WebFetch cannot reach loopback / link-local / metadata addresses; child processes get an allowlisted environment; interactive shell off by default; guard hooks on planner and main agent.
- **No outside MCP**: the ScopeSentry MCP seed, sync page and API are removed; the only seeded MCP is `artex-browser-mcp` (disabled by default).
- **Fully local build**: multi-stage `Dockerfile` from digest-pinned base images (node / golang / python), `npm ci --ignore-scripts`, offline Go build from `vendor/` (`GOPROXY=off`, `GOTOOLCHAIN=local`), image `artex:local` built by compose; no registry image, no NodeSource, no `npm -g`, no Playwright downloads, no `# syntax=` frontend pull. Container runs as non-root `artex` (uid 10001); nmap gets `CAP_NET_RAW` via file capability.
- **install.sh**: no `get.docker.com`, no `docker compose pull`, no host-exposed Postgres; generates `.env` with a random `POSTGRES_PASSWORD` (mode 600).
- **Own browser MCP**: `artex-browser-mcp` drives the system Chromium over the DevTools pipe, replacing `@playwright/mcp`. Google service domains are blocked at the browser's resolver and by a managed policy; `file://` and other non-http(s) URLs are refused; Chromium runs in its own process group with a throwaway profile/HOME that is removed on close, signal or the next start.
- **Container init**: compose sets `init: true` so orphaned helper processes are reaped.
- **Web build hygiene**: Next.js telemetry disabled (`web/.env` + scripts); husky / lint-staged removed; external support link (x.com) removed from the sidebar; fonts are local only.

### Changed (English build)

- **Full Chinese → English translation in place**: UI, server/agent messages, LLM prompts, tool descriptions, skills, docs (`docs/漏洞流量证据.md` → `docs/finding-traffic-evidence.md`) and comments. LLM-facing text was translated faithfully (no rewording) so runs can be compared with the Chinese baseline. Intentional Chinese remains only in test data, legacy matchers that accept both languages, ICP filing examples/regexes, provider quota markers and login-failure regexes.
- **Legacy-compatible data strings**: new intercept verdicts are prefixed `[model]` (`[模型]` still matched); new chat mention tokens use English labels (`@[Finding#12 …]`, Chinese tokens still parsed); "archive not found" and the tool-approval message are matched in both forms.
- **Demo/mock mode removed**: `web/src/lib/mock/` data and handler deleted; `MOCK` is a constant `false`; `NEXT_PUBLIC_MOCK` has no effect.
- **Fix**: `sidequestion` context chunk sizing leaves 16 tokens of rounding slack (a one-token estimate difference rejected the chunk).
- **Chinese baseline**: kept outside this repo as a source tarball and the `artex:baseline-zh` image. Run it with `ARTEX_IMAGE=artex:baseline-zh ARTEX_PULL_POLICY=never docker compose up -d --no-build` on fresh volumes (prompts are seeded on first start only).

### Removed

- ScopeSentry asset sync page and API client; `skills/scopesentry`, `skills/playwright-cli`.
- `.github/` workflows, `docker-compose.bench.yml`, `update.sh`.

## [0.3.15] - 2026-10-07

### Interception

#### Added

- **New built-in interception rule "delete-style API paths"**: previously the built-in HTTP destructive rule only recognized the DELETE **method**, while most applications' delete endpoints can be triggered with GET/POST, so calls like `curl 'http://t/api/user/delete?id=1'` would really delete target data. A `deny` rule now covers `/delete /del /remove /unlink /erase /destroy` (the verb must be followed by a separator, so `/delivery`, `/details`, `/delta` are not wrongly blocked). Existing instances get it after upgrading, and it can be disabled or deleted under "System → Command interception".
- **Token usage of model fallback approval can be viewed separately**: approval calls previously had no separate metering attribution, so how much they cost was guesswork. They are now metered separately, and a usage card is added below the "Model fallback approval" toggle in "System → Command interception" — cumulative stats for call count, input, output, cache read and cache write, plus a mini bar chart of daily usage over the last 30 days.

### Finding notifications

#### Added

- **Findings can be pushed to IM**: a new "System → Notifications" page can push scan findings to six channels: **DingTalk, Feishu, WeCom, generic Webhook, Telegram, email**. Any number of instances of the same type can be configured, each with its own enable/disable, rate limit and filter rules.
- **Two delivery timing modes, real-time and digest**: real-time mode sends each item as soon as it matches; digest mode merges items over a global period (default 30 minutes) into one message, starting with the number of new items and the severity distribution. For "high severity in real time, the rest as a digest", create two channels and configure them separately.
- **Filter rules support four dimensions**: minimum severity, limiting to a task / asset scope, vulnerability class keyword include and exclude (exclude wins), and whether to receive handling status changes (off by default). Messages carry a "View details" button whose destination is set by the global "back-link address" config; if left empty there is no button.
- **Delivery history and manual resend**: lists each delivery's status, attempt count, failure reason and channel, filterable by channel and status; failed items can be resent with one click (resending resets the retry count).
- **Masked echo of channel credentials**: Webhook URLs, signing secrets, Bot Tokens, SMTP passwords and the like are echoed only as masked values with a trailing-characters hint; submitting them back unchanged means "don't modify", and clearing the input box deletes the field.

#### Fixed

> This section is the outcome of a security audit after the first version of the feature was done; each item was reproduced first, then fixed, then covered by a regression test.

- **Fixed masking being bypassable by "change the destination address, keep the credentials" (critical)**: config merging always kept the DB value for "keys not mentioned", so by changing only the address and saying nothing about the credentials, you could make the server send the real credentials from the DB to an arbitrary address, completely silently. Now, once the destination address changes, every credential field must be stated explicitly — give a new value, or explicitly leave it empty to indicate it is no longer needed.
- **Fixed markdown-based channels doing zero escaping of untrusted content**: finding titles and summaries come from model output and asset URLs come from scan results; a crafted title would render as a clickable external link in DingTalk / WeCom / Feishu, and image syntax would be fetched by the client on render, leaking the reader's IP. Content is now uniformly single-lined + markdown metacharacter-escaped, and the escaping happens at each channel's render output rather than in the shared title function.
- **Fixed the SSRF surface of delivery addresses**: previously loopback, internal and cloud metadata addresses could all be delivered to, and the first 200 bytes of a failure response body went into `last_error` and were echoed by the delivery history, forming a semi-blind read primitive. The defense is now at the **dial stage**, covering DNS rebinding, and cross-host redirects are refused; loopback and link-local addresses require explicitly setting `ARTEX_NOTIFY_ALLOW_LOCAL=1`, while RFC1918 private networks are still allowed (self-hosted internal SMTP relays are common).
- **Fixed push credentials leaking via error messages**: DingTalk's `access_token`, WeCom's `key` and Telegram's bot token are all in the URL, and on delivery failure the full URL flowed into four places: `last_error`, the delivery history, server logs and frontend notices. Errors are now uniformly redacted, keeping only `scheme://host` and the underlying cause.
- **Fixed silent loss caused by marking a whole batch as delivered after a digest message was truncated**: truncated findings were neither in the message nor in the failure list, while the delivery history showed success. Messages are now packed **by whole item**, only those that fit in this message are marked, the rest go back to the queue for the next message without consuming retries, and the message header also states how many items will continue in the next message.
- **Fixed a typo in the minimum severity threshold silently disabling the filter**: with a typo like `hgih` for `min_severity`, the unknown level's ordinal was 0 and the check degraded to always-true; users thought they were receiving only high severity, while in fact every finding was flooded into the group. Values are now validated on the write path, listing the options.
- **Fixed `rate_per_min = 0` (no limit) being unreachable**: the DB write layer silently changed an explicit 0 into the default, so operators thought they had lifted the rate limit while it was actually still throttling. The default is now filled in at the API layer when the field is missing.
- **Fixed digest channels completely bypassing the token bucket**: `rate_per_min` previously had no effect on digest mode. The number of items claimed is now constrained by both this round's quota and an in-memory upper bound.
- **Fixed failure handling judging by the whole batch's max attempt count, making old deliveries drag down new ones**: an old delivery already retried twice would drag brand-new deliveries in the same batch into failed, and new findings were lost permanently without a single retry. This is now decided per item.
- **Fixed deliveries whose snapshot could not be parsed in a digest batch being silently marked successful**: such items were skipped in the render stage, yet counted as successful along with the whole batch. They are now explicitly failed with a reason.
- **Fixed the number of items delivered per round for a single channel possibly exceeding the lease duration**: in multi-instance deployments, rows whose lease expired would be claimed again, sent twice and have their attempt count incremented twice. The per-round cap is now derived backwards from "lease / single timeout", with an assertion pinning down the relationship between these constants.
- **Fixed Telegram truncation possibly cutting an HTML entity**: truncation only avoided half tags, not entity fragments like `&amp`, and the parser might reject the **whole** message because of it. Both are now avoided.
- **Fixed the email channel judging temporary SMTP failures as permanent**: 4xx like greylisting `450` are temporary rejections that should be retried later, but were previously always declared dead, so a server with greylisting enabled would make every notification fail after the first attempt. Now distinguished by the first digit of the reply code: 4xx is retryable, 5xx is a permanent failure.
- **Fixed submitting a nested structure actually writing the mask literal into the DB**: object fields like `webhook.headers` can only be masked as a whole or submitted as a whole; a mask sentinel stuffed inside the object would be stored as a real value, causing authentication to silently fail. Such submissions are now explicitly rejected.

#### Design notes

- **The finding write transaction does only one blind INSERT**: `notification_events` is written in the same transaction as the finding, so commit guarantees the two are atomically consistent. This INSERT deliberately does not read the channel table or run the user's filter rules, and is wrapped in a `SAVEPOINT` — otherwise one misconfigured filter condition could abort the transaction and keep a high severity finding from being stored.
- **Delivery uses lease claiming rather than long transactions**: after claiming with `FOR UPDATE SKIP LOCKED`, the row is set to `sending` and `next_attempt_at` is pushed into the future as a lease, so no DB lock is held during delivery; rows left behind by a process crash are claimed again after the lease expires, self-healing without infinite retries.
- **Rate limiting does not consume the retry budget**: this round's quota is first computed from the token bucket, then that many items are claimed. Doing it in the reverse order would make deliveries blocked by the rate limit waste an attempt each, and after the three-attempt budget was used up by pure waiting they would fall into failed.

### Test infrastructure

#### Fixed

- **Fixed permanent leftover assets caused by the cleanup order of the company ICP attribution test**: `defer d.Close()` runs first when the function returns and `t.Cleanup` after it, so all cleanup statements landed on an already closed connection, the errors were discarded, test assets and companies were left in the DB permanently, and other tests asserting on asset counts would fail mysteriously. Closing the connection now also goes through `t.Cleanup` and is registered first, and cleanup failures are made visible.

### Traffic

#### Added

- **Traffic list supports content search, path / status code / length filters and column sorting**: the filter row supports filtering by **response content** (using the full-text index), **path**, **status code** (exact value or buckets like `2xx`) and **response length range**; the table header can sort by time, status code and response length, and the sort preference is remembered locally. Two new indexes take effect automatically after existing instances upgrade; the query contract of the Agent tools is unchanged.
- **New "Clear all" in the traffic list**: deletes all traffic records at once (ignoring the current filters), and cleans up historical host directories no longer recorded in the index; after clearing it also does a full compaction and reports how much space was actually freed. Traffic evidence already bound to findings is stored in a separate evidence store and is not affected.

#### Fixed

- **Fixed disk space not being freed after deleting traffic**: the index DB was created without `auto_vacuum`, so the file never shrank; and the full-text index is `contentless_delete`, where `DELETE` only writes a tombstone without reclaiming the original postings — deleting traffic actually made the index bigger (measured: capturing 6MB of bodies → 16MB index, still 16MB after deleting everything). Newly created index DBs now enable `auto_vacuum=incremental`, and after each deletion a full-text index merge and incremental vacuum run in the background in chunks, gradually returning space to the system (the same scenario drops back to 104KB); the vacuum releases the write lock between chunks and does not block traffic recording.

> Upgrade note: `auto_vacuum` can only be set when the DB is created, so **existing instances' index DBs remain in the old mode**, and incremental vacuum is a no-op on them (a one-line notice is printed at startup). After upgrading, tombstone growth on such DBs has stopped, and the space already used can be reclaimed once with "Clear all" — that step converts the DB to incremental vacuum mode, after which routine deletions take effect on their own.

### LLM

#### Fixed

- **Fixed custom session headers not taking effect on the "one-off LLM call" path**: goal decomposition (round 0) and cold-node compaction are both one-off calls without a transcript store attached, so there was no session id on the context and the gateway could not read the header — for endpoints like opencode zen that return 400 when the header is missing, this showed up as "round 0 fails, while later planner rounds are completely fine". These two paths now explicitly attach a session id that is stable per exploration, which also lets token usage be correctly attributed to the corresponding exploration (previously it was not recorded at all).

### Tools

#### Fixed

- **Fixed output truncation not working for MCP and custom tools, with large results flooding the context**: MCP tool results were previously not truncated at all, and HTTP-style custom tools, because they were passed an empty tool context, always used a hard-coded 6000 characters instead of the session's configured limit. Both paths now go through `actool.Capture`: truncated by the session's `MaxOutputChars` (default 30000), and when `ToolOutputDir` is configured the full output spills to disk, leaving only the leading fragment and a file pointer in the message.
- **Upgraded norma to v0.4.3**: the harness layer uniformly runs `CaptureOnce`, so no tool (including ones added in the future) will overflow the context again by missing truncation.

### Deployment

#### Fixed

- **Fixed the activity feed failing to connect behind a reverse proxy**: the SSE address was previously unconditionally built as `${hostname}:8787`, so in deployments reverse-proxied to 443 the page loaded from `https://domain/` while SSE was pointed at port 8787, which is not open to the public, and the activity feed kept reconnecting. It now branches on `NODE_ENV`: production defaults to same-origin, and only `next dev` keeps `:8787` to bypass Next.js buffering of SSE; the `NEXT_PUBLIC_SSE_BASE` override is kept. The README gained reverse proxy deployment notes.

### Findings

#### Fixed

- **Fixed the asset list in the "By asset" view having no scrollbar when it overflows**: the outer card only had a `max-height` and no definite height, so the scroll viewport's percentage height could not be resolved, and with many assets the list either burst out of the card or was cut off without scrolling. The height cap is now placed directly on the asset tree's native scroll container, adapting to the window height.

### Database

#### Fixed

- **Set limits on the connection pool**: `database/sql` by default does not limit how many connections the application itself can open, and creates new ones unconditionally when the pool has no idle connections, all the way up to PostgreSQL's `max_connections` (default 100) before being refused. It is now capped at 32, and excess queries wait for an idle connection, so the same load shows up as slower rather than as errors; if `max_connections` has been lowered, lower this accordingly.
- **Improved connection reuse**: the default `MaxIdleConns` is 2, so connections beyond 2 concurrent ones were closed after use, and the next request had to redo the TCP and PostgreSQL auth handshake. The idle cap is now raised to match the total cap, with a 30-minute connection lifetime and 5-minute idle eviction added, so the pool can naturally replace long-lived bad connections.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.14] - 2026-09-24

### Task list

#### Added

- **New "Vulnerabilities" column in the task list**: shows the count per severity tier, Critical / High / Medium / Low, with non-zero tiers colored by severity, so each task's vulnerability volume and distribution are clear at a glance.

#### Changed

- **Narrower "Description / Goal" columns**: overlong content is shown truncated, with the full text on hover, reducing the horizontal space long text takes in the list.

### Exploration graph and planning situation overview

#### Changed

- **Slimmed down the planning situation overview (graph_overview) and set count caps on each list**: recent facts, finished intents, pending intents, cold-zone summaries and confirmed finding details all changed to "latest window + count fallback", with what is not shown queryable on demand; this significantly reduces the per-round LLM context size and avoids context bloat in long tasks (the cold zone of the related-task overview is capped as well).

#### Fixed

- **Eliminated reverse duplicate edges between digest and finding in the exploration graph after collapsing cold nodes**.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.13] - 2026-09-19

### Asset interception

#### Added

- **New global asset interception rules (blocklist)**: supports entering exact and fuzzy-match domains / IPs / URLs and CIDR ranges, with create/read/update/delete and enable/disable of rules; the management page is at "System → Asset interception"; built-in defaults fuzzy-block government (`.gov` / `.gov.cn`) and education (`.edu` / `.edu.cn`) sites.
- **Asset interception wired into the execution chain**: before dispatching an intent (`add_intent`) or inserting assets (`insert_assets`), the Agent first runs an interception check on the target assets — intents that hit a block are not dispatched, assets that hit a block are not inserted, and the asset info and block reason are returned to the Agent.
- **New task-level block / allow (allowlist) rules**: independent of global rules and effective only for this task; the evaluation order is "block first, then allow" — a block hit means forbidden; if no block hits but this task has allow rules and none match, it is "not allowed to test"; when no allow rules are configured the allowlist is not enabled. They can be entered when creating a task, or added/edited/deleted and enabled/disabled in the task details "Overview".
- **Task templates support preset category and task-level block/allow rules**: a template can save the task category and a set of task-level rules, which are carried into the new task form when the template is applied.

### Operation review

#### Fixed

- **Tightened the model judge's output protocol to reduce wrongful allows caused by truncation**: lowered the verdict explanation (comment) limit from 500 Chinese characters to 120 Chinese characters and enforced "output only JSON, with no preamble or code block", so a verdict truncated by `MaxTokens` does not become unparseable and then get allowed under the model failure policy (fail-open).

### Task archive

#### Fixed

- **Archiving skips symlinks instead of failing the whole package**: the archive format end to end only supports regular files and directories, and previously any symlink in the working directory made the whole task archive fail; now the symlink is skipped and logged, and the remaining files are archived normally (links are not followed and do not escape the directory tree).

### Account and compliance

#### Added

- **New "Terms of use and disclaimer" dialog before login**: you must check agreement before you can log in.

### License and dependencies

#### Changed

- **The project adopts the AGPL-3.0 open source license**, and the README's license and disclaimer section was improved.
- **Upgraded norma to v0.4.1**.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.12] - 2026-09-17

### Exploration chain broadcast board

#### Added

- **New "Exploration chain broadcast board" on the task details page** (#144): presents the task's exploration nodes (origin/goal/intent/fact/finding/hint/compaction) as a timeline feed, with filtering by type, keyword search, ascending/descending order, pagination and auto-refresh; when on "the first page, newest first" it is the live position and refreshes every round, and when you leave that position it only accumulates an unread count without disturbing what you are reading, with "N new broadcasts · back to latest" to return in one click. Grouped by day, so even when paging through a long task you can tell "which day this happened".
- **Each row of the broadcast board shows the node id** (#145), making it easy to cross-reference the exploration chain graph and locate a specific node.
- **Expanding a node's details in the broadcast board shows upstream/downstream and anchored assets** (#147): expanding a broadcast shows the node's upstream (where it came from) / downstream (what it produced) relations, and hovering a related entry pops up that node's card (type/status/source/time/summary/payload snippet); it also lists the assets the node is anchored to (type label + identifiable text). The related data is sent along with the broadcast page, so expanding makes no extra requests.
- **Broadcast board search supports filtering by node id** (#150): in addition to "content / source", the search box now matches node ids; entering a plain number or the "#41" form shown in the UI locates the corresponding node exactly.

### Intent management

#### Added

- **Intent deletion supports two modes, "soft delete / hard delete"** (#149): pending, running and paused intents can all be deleted, and deletion requires a reason; the deletion mode is chosen in the confirmation dialog.
  - **Soft delete (default)**: the intent is set to "Deleted" and the reason is recorded in a separate field, keeping the intent node and all of its outputs and lineage.
  - **Hard delete**: physically removes the intent, together with the exclusive descendant nodes "supported only by it" (cascading along the output / intent chain down to the leaves), to avoid leaving orphaned data; the deleted intent's token metering is archived and kept by its original date; shared nodes (still referenced by other intents), goals and task root facts are always kept, and the hard delete dialog shows the estimated number of nodes affected by the cascade.

  Both modes notify the planner that "this intent was deleted by the user + the reason", and it replans accordingly.

### Operation review

#### Added

- **Approval records support filtering by status and verdict source** (#139): approval records can be filtered by approval status and verdict source to quickly locate a target record.

#### Fixed

- **Pending approval requests load independently of history pagination** (#133): pending requests are no longer affected by the history list's pagination and stay fully visible while paging through history.

### Agent

#### Changed

- **The Worker role description now uses a general cybersecurity platform wording** (#138).

#### Fixed

- **Fixed cold-node compaction never triggering**: wired a Compactor into the per-task planner; cold-node compaction (cold-digest) previously never ran because it was not wired in, and has now been restored.

### Chat

#### Added

- **Chat supports @ references to multiple record types and scroll pagination** (#135): multiple types of records can be @-referenced in chat, and the reference candidates support scroll-paginated loading.

#### Fixed

- **Long message bubbles are constrained within the conversation panel** (#137): overlong message bubbles no longer overflow the conversation area.
- **Fixed uploading a file before a new conversation is created reporting "missing upload file"**: the Composer's file picker now snapshots the `FileList` into an array before clearing the input and then calls back; previously the draft state had to create the conversation asynchronously first, and by the time execution resumed the `FileList` live-bound to the input had been cleared, so the upload was missing the file field and the backend returned 400.

### Traffic

#### Fixed

- **The traffic recording proxy listens only on 127.0.0.1 by default** (#129, #130): avoids exposing an open proxy on other network interfaces under the default config.

### Web

#### Fixed

- **Fixed the static-export task list page losing the global header**.
- **Added the linked traffic mock for the demo finding details page, fixing a blank screen**.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)
- [@dingpotian](https://github.com/dingpotian)

## [0.3.11] - 2026-09-15

### Operation review

#### Added

- **Approval records support source navigation** (#125): clicking an approval record's "Source" jumps back to the tool execution that triggered that approval — it opens the original full session, auto-paginates to the target position, expands the command and result in place and highlights it centered in the scroll area, with the surrounding messages kept readable; auto-correction stops when the user scrolls manually. Across regular chats, task Workers, the planner and segmented main Agent sessions, it locates precisely via the persisted `tool_use_id` and task mapping (with a new scoped call ID index), and when something is missing, duplicated or ambiguously linked it says so explicitly rather than jumping to another execution. A clear notice is given when the session was deleted or the task archived / the record is missing.
- **Approval records support paging** (#115).

#### Changed

- **Slimmed down model review input** (#124, #125): model review (the LLM judge after no rule matches) input is narrowed to a versioned JSON of "the current complete tool call + explicitly selected short background + the local working directory". Background is taken only from real user messages (the current user message of a chat/task main Agent); Workers no longer attach the intent summary and also clear background inherited from the parent Agent; the planner and auto-triggered sessions do not fabricate user messages. Task description, goals, operation constraints, the global exploration situation, historical calls and the Worker's full intent are no longer attached — these are still used by the original logic for Agent execution and independent session auditing, they just don't go into operation review. All verdicts are required to output JSON `decision`/`comment`, and the explanation must include "the actual operation, the consequences if it succeeds, the matched rule"; instructions inside parameters or background cannot change the review policy. A snapshot and fingerprint of the input actually sent to the model are saved; old snapshots are kept and labeled with their version, and old inputs are not fabricated from current data.

#### Fixed

- **Model verdicts wrapped in a code block are no longer silently allowed** (#126): if the verdict JSON returned by the model was wrapped in a ``` code block, parsing previously failed and it was silently allowed via the model failure policy; now the code fence is stripped first and then parsed, and only if parsing still fails is the configured failure policy applied.

### Agent

#### Added

- **New experimental noa context compaction** (norma upgraded to v0.4.0): an experimental feature that can be enabled in system settings, off by default. When enabled, noa (model-driven context compaction) takes over context compaction for the four Agent types — main Agent, planner, Worker, chat — replacing the built-in compaction; the compacted originals are persisted and archived, centrally under `<workDir>/noa/<session ID>/` (not scattered across task directories), in globally unique directories per session ID. If wiring fails it automatically falls back to built-in compaction without interrupting real tasks; the toggle is read once per run, and switching only affects runs started afterwards.

#### Fixed

- **Exploration graph tools refuse rather than crash on nil when there is no task context**: calling exploration graph tools outside a task context returns a clear error instead of crashing on a nil store dereference.

### MCP

#### Added

- **Support for legacy SSE MCP servers** (#117): compatible with MCP servers that only provide the legacy SSE transport.

### Traffic

#### Fixed

- **Traffic search matches the recorded host in a port-aware way** (#114): `traffic_search` host matching now includes the port, avoiding cross-talk between records of the same host on different ports.
- **An error migrating the `traffic_search` description no longer interrupts the subsequent reporter migration**: a single failed migration is isolated and does not affect subsequent migrations.

### Web

#### Changed

- **Added dividers to the task retest vulnerability options** (#122): dividers between retest vulnerability options for visual clarity.

#### Fixed

- **Fixed the demo task details page crashing entirely**: in mock mode the task details "Sessions" tab fetches `GET /api/tasks/<id>/side-questions` (side question history), but the mock handler had no such route, so the read fallback returned `[]` under "a path ending in s is a collection", making `data.items` `undefined`, and the side hook's `merge()` iterating over it threw `TypeError: t is not iterable`; that exception happened inside the `setItems` updater and was deferred by React to be rethrown in the render phase, where the caller's `catch` could not catch it, and the whole page was taken over by the error boundary showing "This page couldn't load". The mock handler now explicitly returns an empty side history, and `sideAPI.history` also normalizes the return value (`items` that is not an array always falls back to `[]`) as defense in depth.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)

## [0.3.10] - 2026-09-13

### Network

#### Added

- **New DeepSeek official source for web search**: directly reuses the currently active LLM config. It differs in nature from the other three sources — DeepSeek has no directly callable search API, search only exists inside its Anthropic-compatible API (the `web_search_20250305` server-side tool) and is executed by DeepSeek's servers. So it **only supports the DeepSeek official endpoint + the anthropic protocol** (OpenAI-protocol endpoints reject server-side tools outright), each search **consumes an extra model call**, requests **do not go through the search egress proxy** and **are not recorded in the traffic audit trail**, and results contain **only titles and links** (no summaries; use WebFetch when the body is needed). The settings page explains these limits but **does not validate or block**; whether they are met is up to the user, who can verify with a real run via the "Test search" button.

### Agent

#### Added

- **Workers gain cross-work lookback**: new `search_all_worker_traces` (no need to know the intent_id first; keyword search across the execution processes of all works in this task for matching steps) and `get_worker_trace` (after locking onto a work, list its step stream, search by keyword in place, fetch full content by step_id), for reusing observations other works saw but did not write into facts, avoiding duplicated work.
- **`node_detail` made available to workers**: together with the lookback tools above, a worker that has an intent_id / node id can directly look up that node's full details.
- **Planning rounds triggered by `add_hint` are explicitly announced**: previously a new hint was only folded into the situation overview for the planner to discover itself; now one `add_hint` records a trigger for the planner "a human added N strategic hints: …" (a batch counts as one, not flooding item by item), and the planner is explicitly told "this round was triggered by new hints" and sees the hint content directly.

#### Changed

- **The global situation prompt now uses a "loosen up" tone**: encouraging divergent exploration and timely reporting of cross-intent leads, rather than premature convergence.
- **Slimmed down the worker boundary wording**: an initial block does not mean the avenue is exhausted; focus on "exhausting the bypass techniques within this intent before concluding".
- `insert_assets` removed the per-asset `related` input: its only purpose was to decide whether to add the asset to this task's scope, yet the value was not stored (it was voided when the same asset was registered again, and the UI did not show which ones were judged unrelated), so in practice it just made the model make one more judgment that could not be kept.
- **The test scope (`task_scope`) is no longer affected by the asset coverage toggle**: `insert_assets`' automatic scope addition (`source='auto'`) runs whether coverage is on or not, and `add_task_scope` is always provided to the planner / in-task main Agent / goal decomposition Agent. The scope is the task's authorization boundary and the filter basis for asset queries; the coverage toggle only decides whether to use it as the denominator for metrics, and should not decide whether the scope itself accumulates. Previously turning coverage off disabled both the `auto` and `agent` write paths at once, leaving `task_scope` with only rows added manually in the UI. `list_untested_assets` is still hidden when coverage is off (it is a purely coverage-oriented view).

#### Fixed

- **Fixed assets leaking between tasks** (#59): `list_assets` previously hard-coded the task id to 0 and queried the whole shared asset store raw, while the model had no way to write a scope filter, so it took other tasks' assets (especially IPs) as this task's targets and the test direction was led astray. It now returns only assets that fall within the test scope (`task_scope`) of [this task and directly related tasks], matching by **ownership** rather than literal value: if the scope has a root domain, all subdomains/services/endpoints under it can be found; if it has a network range, hosts and services within the range can be found; fetching an out-of-scope asset directly by id likewise returns nothing. Non-task contexts (Auto/pentest) have no scope to rely on and still fall back to the whole store. This also fixes the ownership blind spot where direct-IP hosts (e.g. `http://1.2.3.4/api`) could not be matched by a network range scope. The UI's "Tested assets" view (filtered by task producer) is not affected.
- **Fixed the workers' cross-work lookback tools being wrongly removed**: after `search_all_worker_traces` / `get_worker_trace` (and `node_detail`) were added to the worker default tool set, an old migration that "narrowed the worker tool surface" unbound them again at startup, so workers did not actually get these tools. They have been removed from that unbind list, and DBs that already ran the old migration get them re-bound to workers once.
- **Fixed occasional DB write failures for `/btw` side questions**: the side_question checkpoint now strips NUL (`\u0000`) escapes, which JSONB does not support, before being stored, avoiding write errors for content containing that character.

### Triggers

#### Fixed

- **Merged trigger sessions deduplicate task description/goals per task**: when multiple tasks are triggered together, the same task's description/goals are no longer concatenated into the message repeatedly, avoiding long goals piling up and blowing up the context.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.9] - 2026-09-11

### Agent

#### Added

- The in-task main Agent supports **multiple sessions**: you can create, switch and independently reset context, and every session is interactive.
- Support for persistent `/btw` **side questions**: ask follow-ups without interrupting the main thread; Q&A is stored in the DB and survives restarts.
- `spawn_task` adds `source_task_ids`, so a new task can **read-only inherit** the source tasks' assets and findings.

#### Changed

- Disabled workers' cross-engagement memory and narrowed their default tool surface: reading context and cross-work review are planning responsibilities; a worker only handles executing and writing back a single intent.
- Slimmed down the worker default prompt; `get_worker_output` removed the `terminated` and `worker_name` fields; slimmed down the descriptions of `insert_assets` / `list_assets`.

#### Fixed

- Fixed works being interrupted early by **idle turns**: the model sometimes spends a whole turn outputting only thinking, giving neither text nor tool calls; the harness then sees a natural end (`end_turn` with no `tool_use`) and wraps up directly with `completed` + an empty summary — an intent that wasn't finished is cut off halfway, showing up as "the task ended normally but there is no text summary at all". None of the five layers of LLM retry could reach it: it is not an error; the entry point of same-provider safe-window retry is "stream failure", the circuit breaker judges success directly when `err == nil`, and intent rerun only recognizes `model_error`; the SDK's empty-response retry can't reach it either, because that layer judges emptiness by "whether any event was yielded", and thinking deltas are themselves events. Now the work's Stop hook recognizes such turns and injects a continue instruction, letting the model carry on to the next step with the thinking it already produced — "resend as-is" was deliberately not chosen, because such idling is usually determined by the prompt and context shape and is stable behavior rather than a random glitch, so resending would only make the model think it through again.
- The count reuses the **empty-response retry count** under "Retry and backoff" on the LLM page (both answer "the model finished but produced no substantive content", only with different criteria and means), default 2; set `-1` to turn it off and fall back to the original "idle means wrap up" behavior. It is a **total** cap per intent rather than a consecutive count: the harness itself already limits consecutive idling to a single nudge, and the quota is only refreshed when a real tool turn has happened, so this number guards against loops like "tool → idle → nudge → tool → idle" exhausting the intent's budget. When triggered, the log records `[work <worker> · #<intent>] idle turn…injecting continue instruction (n/N)`. See `docs/LLM重试设计.md` (LLM retry design) §1.1 for the design.
- Fixed `/btw` context budget and input layout in long conversations; in non-secure contexts (non-HTTPS access) side request IDs are generated with a fallback.

### Traffic

#### Added

- Findings support **linking multiple pieces of traffic evidence**, which can be sorted, annotated and distinguished by role (request/response/corroboration).
- The report Agent **automatically links** relevant traffic evidence before writing a report.
- New Agent **traffic binding toggle**, and completed the handoff of evidence between Agents.

### Tasks

#### Added

- Support for **session-level vulnerability retest**: retests run in a separate Agent session, and the list and details show the run status.

### Interception

#### Added

- Approval records gain **details and execution audit**: view the tool request context, the model/rule initial verdict, execution output and parameter fingerprint.

### Assets

#### Added

- Task tested assets support **DSL search**.

### LLM

#### Fixed

- Fixed the connection test not sending the custom session header, which caused opencode zen to return 400.

### Network

#### Added

- MCP's HTTP transport supports **skipping TLS certificate verification**, making it easier to connect services with self-signed certificates.

### UI

#### Added

- The session list is grouped by Agent, with expand/collapse and independent pinning; conversations can be filtered by Agent.
- The attack chain graph renders compaction (digest) nodes and collapses their members.

#### Fixed

- Fixed a blank screen caused by out-of-sync login credentials.
- The interception notice text now explicitly marks "Platform governance", to avoid being misread as a defense on the target side.

### Deployment and updates

#### Added

- Support for **one-click update from the UI**: the system config page gains a "Version and updates" card, and the top bar lights up a notice when a new version is available. The flow is download the release package → verify `SHA256SUMS` → smoke test → stage → exit and have the supervisor script relaunch to complete the swap, with the page refreshing automatically.
- New supervisor launch scripts `start.sh` / `start.bat` as the official entry point (included in both the release package and the Docker image; `install.sh` is unchanged), which decide whether to relaunch based on the exit code and forward SIGTERM to artex. The verification and swap logic is all in Go, keeping the scripts dead simple.
- Automatic fallback on failure: if verification or the smoke test fails, the new version is discarded and the current one keeps running; if the new version fails to start 3 times in a row, it rolls back to the previous version. The settings page also has a manual rollback (note that the DB schema is not rolled back).
- Updates only trust GitHub domains and enforce HTTPS, and the release source is not configurable; one-click update is disabled for dev builds. GitHub query results are cached for 30 minutes to keep the top bar notice from exhausting the API quota.

#### Known limitations

- Under Docker only the program is swapped, not the image: the toolchain is not upgraded along with it, and recreating the container reverts to the version bundled in the image; use `docker compose pull artex` when needed.
- `skills/` in the release package is not synced, so built-in skills added in a new version do not take effect automatically.
- Updating means restarting, which interrupts running tasks.

### Dependencies

#### Changed

- Upgraded norma to v0.3.7.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)

## [0.3.8] - 2026-09-09

### LLM

#### Added

- The LLM page gains a "Retry and backoff" tab: both the **count** and **interval** of the five retry layers are configurable. A model call's failure passes, from inner to outer, through connection retry (SDK; connection resets / timeouts / 429 / 5xx before the stream starts), empty-response retry (SDK; ended normally but with no content at all, openai format only), same-provider safe-window retry (replay of a dropped stream before any output was delivered to the caller), round-robin circuit breaking (cool down and skip after consecutive failures reach the threshold), and intent rerun (rerun the whole intent after a worker ends with model_error) — five layers, with the outer layer only getting its turn once the inner one is exhausted. Each layer has two knobs with uniform semantics: empty = use the original default count and exponential backoff; a count uses that count; an interval replaces exponential backoff with a fixed interval; -1 = turn this retry layer off. The first three layers follow the endpoint and can be overridden field by field in each model config over the global default (pinning only the interval still inherits the global count); circuit breaking and intent rerun have process-level semantics and only have one global copy. Takes effect hot on save, no restart needed. All empty is the current behavior, and old DBs are byte-for-byte unchanged after upgrading (new columns default to 0, and a missing settings key means all defaults). The connection test deliberately does not carry these parameters — it has a 30s hard timeout, and stacking user-configured retries on top would only make a usable endpoint test as a timeout failure. See `docs/LLM重试设计.md` (LLM retry design) for the design.
- Each LLM config supports a custom **session header** (`session_header_key`): when non-empty, every LLM request carries that HTTP header, whose value is the current run's session id (`conv-<id>` for chat sessions, `exp<x>-worker-i<intent>` for workers, etc.), for gateways that do prompt caching / sticky routing based on a session-id header. Implemented by reading the session id from the request context and injecting it, with no norma changes, so the same shared provider can send different header values per session. Old DBs get a migration, and the frontend LLM config dialog gains an input box.

#### Fixed

- Fixed saving an LLM config failing with `23502` directly when the session header field is empty: the column is `NOT NULL DEFAULT ''`, and previously `NULLIF($n,'')` was wrongly applied, writing an unfilled session header as NULL and triggering the not-null constraint; it is now passed directly with empty-string semantics.

### Agent

#### Added

- Wall-clock timeout now **wraps up in place**: relying on norma v0.3.6, when `MaxDuration` is reached the running tool is interrupted and the run continues on a live ctx for the wrap-up rounds (writing back identified content + summary, with final state timeout), no longer relying on the worker/planner's own external hard ctx of `maxDur+90s` to kill a stuck run outright as `aborted_tools`. Chat goes through the same harness and automatically gets the same behavior; mainagent has no `MaxDuration` and is unaffected.
- Stall fallback: when the planner is woken by heartbeat / no change and the whole graph no longer has any open or running intents, the opening line becomes a stall alert, explicitly stating that no worker is running and no direction is queued, and that this round must produce one or more non-duplicate new intents (producing 0 intents is not allowed).

#### Changed

- Worker prompt restructured: the intent / launch instruction / anchored asset raw JSON moved into the system prompt, reassembled each round and never compacted away, so continuing also does not depend on the transcript's first message surviving; the launch user message is slimmed down to only the global situation overview (degradable, tolerating staleness). The cost is that the system prompt mixes in per-intent data and loses cross-intent cache reuse — a deliberate trade-off for "never losing the intent".
- Slimmed down the planner / worker default bodies and fixed several real-world problems: the planner distinguishes `recent_done` by status (blocked/exhausted: check the trace first before deciding, neither treating it as a dead end nor blindly rerunning), negative conclusions are now "observation / doubtful, not definitive" and the evidence is checked before accepting them, if the goal is not reached and there are no open/running intents it must produce some (hard floor), depth takes priority over coverage; worker negative conclusions only write "observation + tentative reading" with judgment belonging to the planner, and cross-intent leads are written into the fact's summary for the planner rather than chased by the worker. Reseed appends the new defaults as a new version and switches to it, and user customizations / old versions are kept in history for rollback.
- Narrowed the work agent's default tool set and asset write-back prompts: a worker only handles executing and writing back a single intent, while reading context / cross-work review are planning responsibilities, so `list_facts` / `node_detail` / `list_companies` and cross-work search (`search_all_worker_traces` / `list_worker_traces` / `get_worker_trace`) are removed from its defaults, leaving only `list_findings` (dedupe check before reporting a finding) + `add_finding` / `record_fact` + `insert_assets` / `list_assets`; the prompt drops stale field descriptions that conflicted with the `insert_assets` schema (`type=tech`/`on_url`/`props`). Old DBs get a one-time migration stripping the corresponding bindings; the planner/main bindings of the same names are untouched.

### Exploration graph

#### Added

- **Exploration graph cold-node compaction (cold-digest)**: folds old, long-inactive intents / facts into digest nodes shown in `graph_overview`; the original nodes are kept permanently and can be fully restored by id (lossless storage, only the presentation is compacted, folding is reversible). Hot/cold is judged by reverse reachability + any live branch means hot, with R=6 rounds of debouncing and connected-component grouping; background compaction (minor folds uncovered cold blocks, major goes back to the source to recompact and merge fragments) has activity rechecks and cooldown mutual exclusion, never touches the hot path and never overwrites revived nodes. The overview side provides `cold_digests` and an asset-indexed `cold_index`, with `expand_digest` / `expand_index` responsible for restoring. The overviews of related / inherited tasks likewise reuse their own folded views, and `expand_digest` supports cross-task read-only restoring. `expand_digest` / `expand_index` are only given to the planner and main agent, not to workers. All come with old-DB migrations.
- `graph_overview` outputs the full `finding_list`: findings are the task's highest-value output and a single task usually doesn't have many, so they are now included in full in the overview (unlike facts, which only get the recent window), letting the planner/worker see all confirmed findings at a glance each round without calling `list_findings` again. Each item is slimmed down to `{id, summary, evidence?, from_intent?, assets?}`, where affected assets are given directly as readable content (url / domain / ip:port) rather than bare ids.

#### Changed

- `graph_overview` no longer lists `hosts` flat: the coverage block removes the host list (in large-scope tasks each round carried up to 500 host strings, of limited value to planning decisions), keeping only `host_count`, with specific hosts queried on demand via `list_assets`. Adds a top-level `done_intents_total` (total finished intents), parallel to `recent_done_intents`, which is truncated to ≤15, so the planner knows whether anything was cut when deduplicating.

### Tools

#### Changed

- `add_company_scope`'s default binding changed from worker to planner: defining a company's asset scope is the responsibility of planning / main control / Auto, while the worker only executes exploration. New DBs seed the default binding as mainagent/planner/auto, and old DBs get a one-time migration.
- The planner is bound to `list_assets` by default: it now has both `list_assets` (DSL search of the whole store) and `list_untested_assets` (untested within scope); old DBs get a one-time backfill that does not override user unbindings.

### Tasks

#### Added

- The task list gains a "Running Workers" column: counts the intent nodes with `state='running'` under the task, using exactly the same definition as "Running Workers" in the task details overview, and shows 0 when no Worker is running.

### Network

#### Added

- New **global egress proxy** config: all target traffic can go out through a unified global proxy (http/https/socks5, supporting `user:pass`). With traffic capture on, it serves as the upstream of the MITM recording proxy (traffic is recorded as usual and then goes out via the proxy; both the interception and passthrough paths use the upstream, without leaking the source IP); with capture off, it is injected directly into the agent's bash environment and WebFetch (`proxyEnv` adds `ALL_PROXY` to support socks5). The config is stored in the settings KV table (no migration needed), and the frontend system config page gains a "Global proxy" card, independent of the web search proxy and the LLM proxy.

### UI

#### Fixed

- Corrected semantic errors in the intent status labels: `exhausted` "Exhausted" → "Budget exhausted" (it actually means it was cut off midway by the step / time budget and only wrote back partial results, not that the direction was fully explored), `blocked` "Blocked" → "Execution error" (it actually means model / API / network failures exhausted retries and the intent basically never really got explored, not that it was blocked by the target / WAF), and added the previously missing `stopped` "Stopped" (a work manually stopped by the user).

### Dependencies

#### Changed

- Upgraded norma to v0.3.4: MCP tool output gains truncation and spilling to disk (see `651b961`), and the later v0.3.6 supports wall-clock wrap-up in place.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.7] - 2026-08-31

### LLM

#### Added

- Model configs gain an "Output limit", with a choice of which request field name to use for it: the limit caps how many tokens a single reply can generate at most and is sent with every request; 0 (default) = don't send the field and let the server default decide; it is a different thing from the "Context window" — the latter is the model's total capacity, only used locally to compute the compaction threshold, and does not appear in requests. The field name toggle only matters for the `openai` (Chat Completions) format: empty (default) sends `max_tokens`, which the vast majority of compatible gateways only recognize; OpenAI's official reasoning models (o series / GPT-5) conversely only recognize `max_completion_tokens` and reject `max_tokens` outright with `unsupported_parameter`, so such endpoints need to be switched to the new field manually. The two other formats each have a fixed field name (`max_tokens` for Anthropic, `max_output_tokens` for the Responses API), so for non-openai formats the toggle is grayed out and cleared on save. This also completes a path that had never been wired up: none of the five places — planner / worker / chat / main Agent / goal decomposition — had ever set an output limit, so the openai family never sent it at all and Anthropic used the SDK's 8192 fallback; just adding the config option without wiring this up would mean a filled-in number had no effect; now, like "Streaming output", the value is resolved from the config each round, so after a failover switch it takes effect on the next round. Old DBs behave exactly the same after upgrading (new columns default to 0 and an empty field name).
- When clicking "Test" in the backend, each HTTP attempt's status code and the gateway's raw response body are written to the server log (response body trimmed to 4K), making it easy to diagnose 401s, quota messages, empty frames, returned HTML pages and so on, instead of only seeing the ok / err folded in the UI. This log does not depend on the "LLM recording" toggle.

#### Changed

- The task LLM config chain can now be modified in any state, no longer restricted to "running / paused / config chain exhausted": after a task finishes (done / failed / timeout), main Agent chat still uses this chain, and when a model on the chain had problems it previously could neither be changed nor could interaction continue. The terminal-state checks in both the backend HTTP and DB transaction are removed together, and the frontend dialog also opens editing and saving for terminal-state tasks. Saving a terminal-state task no longer reopens quota-blocked intents (those intents would be moved to open, with neither a worker to run them nor still satisfying the "rerun intent" condition); to keep running, still use rerun intent / add goal, which bring the task back to the running state.

### Agent

#### Changed

- "Send a message to a running Worker to adjust direction" now reuses the existing pause / resume + transcript resume mechanism, behaving the same as main Agent chat: previously it went through a self-built intervention persistence protocol that touched the scheduling barrier, the resume flow and over a dozen activity query filters. Now the message is injected via the next round's input, and the intent runs directly in a dedicated goroutine, not limited by the 3-slot worker pool, running as soon as it is sent; the frontend keeps the message box and restores the "Continue directly" button, with sending now carried over SSE. No DB schema changes. Trade-off: messages are kept only in memory with no crash recovery, and sending a message may momentarily put the task one worker over its concurrency (a low-frequency scenario, acceptable).

### Tasks

#### Fixed

- Fixed out-of-memory (OOM) crashes when archiving large tasks: archiving now writes the snapshot as a stream instead of reading the whole task into memory at once; it also closes three recovery gaps on the cold archive path — in modern installs traffic only exists in SQLite, and previously a crash between the PostgreSQL commit and the SQLite commit would leave an archived task's traffic stranded in hot storage with no way to recover it; now a staging journal is written whether or not there is a historical directory, and a missing `journal.json` is also treated as discardable.
- Fixed the system lagging when there are many tasks: loading conversations and loading the UI had noticeable delays. The queries for the task list, task context and exploration records were optimized together, and the frontend dashboard, conversation page and task details page also reduced duplicate requests.

### Assets

#### Changed

- Removed the "at most 256 rules" limit on company asset scopes: companies that enter their scope IP by IP / domain by domain easily hit this cap, after which they could only be split into multiple companies, while the scope itself is not slower just because it has more entries. The cap is removed in the frontend, backend and demo mock together; a single rule is still limited to 1024 characters, and the 2 MiB request body limit is kept as a backstop (roughly forty to fifty thousand rules).

### Skills

#### Fixed

- Fixed uploading a skill archive failing with "Upload failed: zip: unsupported compression": the Go standard library only has two built-in decompressors, Store / Deflate, so bzip2 and Zstandard archives written by compression software at non-default levels could never be extracted. These two decompressors are now added (pure Go implementations, no new external dependencies); for methods that genuinely can't be extracted, such as Deflate64 / LZMA / XZ / PPMd, as well as encrypted archives, a Chinese-language message is now reported before extraction, naming exactly which file used which compression method and how to repackage it, instead of throwing the underlying English error at the user.
- Fixed the in-skill file name validation treating Chinese file names as illegal: path validation was previously an ASCII allowlist of `[A-Za-z0-9-_./]`, so if an archive contained even one file with a Chinese name (something like `参考/说明.md`), the whole upload failed with "archive contains an illegal path". It is now a Unicode blocklist: file names in any language and spaces are allowed, while still rejecting control characters, invalid UTF-8, zero-width and bidirectional control characters (RLO file name spoofing), `\ % # ? * : " < > |` as well as `..` / absolute paths / empty path segments, with directory traversal protection unchanged. Skill names are opened up as well — ASCII is still limited to lowercase letters, digits and hyphens (the agentskills.io spec), while non-ASCII letters such as Chinese can be used directly as skill names, but spaces, dots and path separators are not accepted.
- Fixed Chinese archives made by Windows compression software having garbled file names after extraction or being rejected outright: such zips don't set the UTF-8 flag and write file names in GBK; they are now decoded with a GBK fallback before path validation. Quoted frontmatter like `name: "中文技能"` also yields the skill name correctly now.

### UI

#### Added

- The findings page gains an "All findings" flat view, set as the default, switchable with the original "Grouped by task" view via a header tab: in the grouped view you have to expand task by task to see vulnerabilities, which is a detour when you just want to glance over the list across tasks. The flat view is one big cross-task table (10/20/50/100 per page), behaving exactly the same as the grouped view — check to export, expand inline to see evidence and the detailed report, inline edit of name/category/severity, change handling status, dig deeper, delete. The two views share the stats cards and filter bar, switching doesn't lose filters, and the current view and filters are remembered locally together; polling only hits the current view, and inline changes write to both caches so switching over won't show stale data.
- The findings page gains a "By asset" view: an asset tree on the left (company → root domain / IP → subdomain → service → endpoint; the company level only appears when the assets actually have an owner), and on the right the findings under the whole subtree of the selected node, sharing the same table and the same set of filter conditions with the other two views. The tree only includes assets that have findings, with the ancestor chain filled in as needed (when a finding is only attached to the deepest endpoint, the levels above can still be assembled), and the counts on nodes are subtree aggregates deduplicated by finding; findings whose asset was deleted or that never had a linked asset go into an "Unlinked assets" bucket. Unlike the other two views, the asset view doesn't poll — it only queries when entering the view, changing filters, adding/deleting/editing findings in the page, or clicking the refresh button on the tree; the left tree is a navigation structure and there is no need to recompute it every 5 seconds. Each level of the tree only shows the increment relative to the level above (subdomains drop the root domain suffix, services show `https :443`, endpoints show only the path), with the full value in the hover tooltip and the breadcrumb; when there are too many nodes, the endpoint/service levels are dropped for that whole level with a notice, and counts are still included in the level above.

#### Fixed

- Fixed session records being squeezed and not fully visible in session details on mobile: on narrow screens the session list is collapsed, giving the width to the records themselves.

### Dependencies

#### Changed

- Upgraded norma v0.3.2 → v0.3.3: adds `Config.MaxTokensField`, letting the OpenAI Chat Completions output limit be sent as `max_completion_tokens` instead (reasoning models only recognize this key). The two keys are mutually exclusive and only one is sent; the default is still `max_tokens`.
- Upgraded `golang.org/x/mod` v0.37.0 → v0.40.0, fixing two dependency security alerts.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)
- [@begininvoke](https://github.com/begininvoke)

## [0.3.6] - 2026-08-27

### LLM

#### Fixed

- Fixed the "non-streaming" toggle reverting to "streaming" when reopened after saving (#69): the config list API's DTO was missing the `streaming` field, so the response never returned it, and the frontend, reading `undefined`, always fell back to the default streaming; the actual write and DB storage were fine, it just couldn't be read back. The DTO gets the field back (without `omitempty`; `false` must also appear in the response).
- The connection test now verifies that the model actually replies, and tests using the config's real send/receive mode (#65): previously it only judged whether HTTP succeeded, so when the request went through but the model replied with nothing (thinking burned the budget / the body was swallowed by a safety policy / the compatibility layer dropped `content`), it still reported "Connection successful", at odds with the "doesn't reply" behavior in sessions; and the test always used streaming, so a non-streaming config was actually testing a different channel. Now an empty reply is judged a failure outright, the model's reply is shown in the notice on success, and the current streaming toggle is carried into the test, keeping "test passed" consistent with "the session works".

### Agent

#### Changed

- `list_facts` now uses pagination + keyword filtering, avoiding a single call blowing up the context when there are many facts (#74): it returns the latest 20 by default, supports `limit` (max 100) / `before` cursor / `q` summary keyword, and returns `{facts, total, has_more, next_before}`; an overlong single summary is truncated by character count (the full text is still available via `node_detail`). The worker / planner prompts are updated to the pagination semantics. Old DBs get a one-time migration that refreshes the new parameter schema into the tool catalog table (`SeedTool` only inserts the first time; otherwise the tool management page shows "No parameters").

### Tools

#### Added

- The "Tool executions" page gains tool call count stats (#72): the toolbar's "Stats" button opens a dialog showing call count, share and failure count per tool (sorted by count descending); it reuses the list's task / keyword filters, counts the whole result set rather than the current page, and only fetches when the dialog opens.

### Dependencies

#### Changed

- Upgraded norma v0.3.1 → v0.3.2: fixes reasoning/refusal being silently dropped in OpenAI-family responses (Responses gains `reasoning_text`), dedupes the three names of the reasoning field, bounded retry for empty responses (zero content blocks), and gateway 400s caused by compaction boundaries cutting tool pairs (also fixing existing orphans already baked into transcripts).

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.5] - 2026-08-25

### LLM

#### Added

- Each LLM config supports switching between streaming and non-streaming (streaming by default): on uses streaming SSE; off uses true non-streaming (`stream:false`, returning the full JSON at once), which can bypass some gateways' poor SSE implementations (empty frames, dropped thinking-field frames), at the cost of losing real-time progress and real-time token counts while running. worker / planner / mainagent / chat / goals all read the value dynamically from the currently active config; the three Provider wrapping layers `llmpool` / `llmrec` / task runtime are all compatible with non-streaming; `llm_profiles` gains a `streaming` column, with an `ALTER` to backfill old DBs (default `true`, transparent to old configs).
- Support for LLM configs in the OpenAI Responses API format: each config gains a third format, `openai-responses` (hitting `POST /v1/responses`), alongside Chat Completions / Anthropic; `BaseURL` is normalized and the default model is `gpt-5`; the `format` constraint of `llm_profiles` adds `openai-responses`, with an idempotent migration to backfill old DBs; the frontend format dropdown gains "OpenAI (Responses API)". Dependency upgraded to norma v0.3.1 (including the `reasoning_content` passback fix).
- Recording the raw HTTP of LLM requests/responses: the real wire body is captured at the HTTP transport layer, keeping the tool schemas, `tool_use` blocks and raw SSE frames not visible in the normalized view; multiple attempts from norma's internal retries are each kept; `llm_records` gains two columns, `raw_request` / `raw_response`, with an `ALTER` to backfill old DBs; the recording page's details panel adds a "Raw" view toggle and request/response copy buttons (with an `execCommand` fallback compatible with non-secure contexts).

### Conversations

#### Changed

- Multi-select in the session list is now a "Multi-select" mode toggle: by default the list no longer has a permanent checkbox on every row (a cleaner look), and the header becomes "N total + Multi-select" button; clicking "Multi-select" enters selection mode (checkboxes, select all, batch delete), "Done" exits and clears the selection, and after a batch delete fully succeeds it exits automatically (if anything failed it stays in selection mode to make retrying easy); single-item rename / pin / delete still go through each row's ⋯ menu.

### UI

#### Changed

- Improved task management, session operations and traffic viewing (#57): the task list column sort preference can be persisted, inline rename is now triggered directly by an icon (no longer via a menu), and sheet interactions and traffic viewing details are improved.

#### Fixed

- Worker asset labels only show domains and IPs, and correctly handle the empty label case.

### Agent

#### Added

- The planner gains a "quantitative acceptance check" when judging goals: when a goal contains quantifiable acceptance criteria (asset test coverage reaching X%, getting N flags, obtaining some privilege), before `prove_goal` it must check the measured values in `graph_overview` (`coverage.pct`, finding counts, etc.); if the measured value doesn't meet the bar, `prove_goal` is forbidden and it must dispatch intents to close the gap instead, and must not mark it met early on the grounds that "the main part is done / it's broadly achieved". Fixes a goal requiring 100% coverage being judged complete when the measured value was only 40%.

#### Changed

- Rewrote the basis for judging "0 intents this round" in the planner prompt: the original text described 0 intents as "the most common, most important principle", which would make the planner stop too early when the goal was not reached and there were still untested surfaces in scope. It now says 0 intents should only be produced in two specific situations — ① every direction thought of is already covered by open/running/recent_done intents; ② the next step depends on the output of a currently running work, and that output has not appeared yet (it should wait for it to finish and plan at the next wakeup after the graph updates). And it adds a reverse constraint: when there genuinely is a new direction that is not covered and doesn't depend on a running work, or the goal is not reached and there are still untested surfaces, don't stop because "0 intents is common".
- Strengthened the "evidence bar for negative conclusions" in the worker prompt: for negative conclusions like "not injectable / port closed / no login entry" that could make the planner abandon a whole direction, it requires exhausting the reasonable techniques within the intent (change encoding/parameters/paths/methods) before concluding; if the techniques were not exhausted or the evidence is weak, always mark `confidence=inferred`, to avoid welding a whole route shut with a careless `observed` negative (wrong negatives early in a task especially lead the direction astray and are hard to self-heal).
- The above planner / worker default prompt changes are appended as new versions and switched to as the current version via one-time migrations (`reseedPlannerPrompt` / `reseedWorkerPrompt`, each guarded by a settings flag); old versions are kept in the version history, and users who customized their prompts can restore them from the version records.

### Operations

#### Added

- New `reset-password.sh` to reset the admin (username fixed as `ARTEX`) password: supports both local / docker deployments, with connection info specified explicitly or read automatically from `--dsn`/`$ARTEX_PG_DSN`/`config.json`; uses `pgcrypto` in the DB to generate a bcrypt hash compatible with backend login and writes it back to `settings.auth.password_hash`, with no service restart needed after the reset. The password is passed via an environment variable and does not enter the process argv, and is escaped to prevent injection.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)

## [0.3.4] - 2026-08-24

### UI

#### Added

- After checking multiple tasks in the task list, their category can be changed in bulk, and the target category supports "Uncategorized" to move tasks out of their current category; the whole batch is written in one transaction, and the only possible failed items are tasks deleted after being checked.

#### Changed

- New task's "Task category" changed from a dropdown to a searchable input: typing filters existing categories, and a name not in the DB is created and selected instantly on Enter (or by clicking "Create" in the dropdown), with the selected item shown as a removable tag; still limited to a single category.

### Conversations

#### Fixed

- Fixed sending a message being blocked by "LLM not configured, cannot chat" when the session has an LLM config selected but there is no globally active config. The pre-send check only looked at the global active config, while actual execution prefers the session's selected config, so the two resolution logics were inconsistent; they are now unified into one (session/Agent binding first, global as fallback).
- The notice when chat is unavailable is broken down by state: no config at all prompts "Add a config", configs that exist but are not active prompt "Activate one or assign a config to this session", instead of always showing "Not configured", making it easier to pinpoint.

### Agent

#### Added

- For tasks whose goals are all achieved, intents dispatched by the main Agent via `add_intent` can be claimed and executed directly by Workers and stop once done: in this case the Planner no longer runs (a task with no open goals doesn't enter planning), avoiding it re-judging the goal as achieved and canceling the just-dispatched intent; once the frontier is drained the task returns to completed. Before dispatching an intent, the main Agent asks the user, based on the intent's content, whether to register it as a formal goal; if registered, the task resumes regular autonomous planning.

#### Changed

- When `get_worker_trace` / `get_task_worker_trace`'s `step_ids` exceed the per-call cap (5), it no longer errors outright, and instead returns the full content of the first 5 steps, with `returned_step_ids` / `omitted_step_ids` / `notice` in the result saying which were fetched this time and which were not, and a hint that "if this is already enough, no need to fetch more". Duplicate and invalid ids passed in are deduplicated and removed before counting.

#### Fixed

- The task list now sorts by task id descending (newest created first), replacing the previous sort by creation time. Multiple tasks created at the same moment had the same timestamp and an unstable sort, which, combined with polling every 10 seconds and the random iteration of an in-memory map, caused them to frequently swap positions in the list.

### Assets

#### Fixed

- Fixed company ownership recalculation being broken by a single row of dirty data: when `assets.ip` stored a hostname (written by the Agent or the asset API), `a.ip::inet` threw `22P02`, making adding/modifying company asset scopes and deleting companies all fail and roll back. Now uses the safe conversion `try_inet()`, skipping invalid values instead of aborting the whole statement.
- Unparseable `ip` values are no longer silently skipped: after a company scope is saved, it explicitly reports how many assets have an `ip` that is not a valid IP (with the specific ids and values), as a reminder that these assets won't be attributed by IP/CIDR rules; the same information is also written to the server log, covering paths with no frontend response (deleting a company, scopesentry sync, Agent writes).
- Task test scope IP/CIDR matching also switches to `try_inet()`, replacing the previous regex guard that only checked the character set (hostnames made entirely of hex letters, like `abc.def`, previously still slipped through and triggered the same error).

#### Changed

- The `ip` field of `ip`, `service` and `endpoint` assets no longer accepts hostnames: `insert_assets` and the asset API return an error per item with its `index`, and directly give the fix (use `type=subdomain` and fill in `domain` instead, or resolve the A/AAAA record first), so the Agent can correct it itself and reinsert. Other assets in the same batch are unaffected and are stored as usual.

### Contributors

- [@neouks](https://github.com/neouks)

## [0.3.3] - 2026-08-23

### Worker

#### Added

- Running Workers support individual pause, resume and cancel; pause keeps intents, facts and findings, while cancel transactionally cleans up the current intent and its direct outputs after execution exits.
- Added the `paused` intent state, an execution fence and named termination reasons, preventing late blackboard writes after pause, cancel or task deletion.
- Added an optional task concurrency cap; creation, resume, finding deep-dive and queue backfill all go through a unified persistent FIFO admission path.

#### Changed

- The default wall-clock duration of a single Worker run changed from 600 seconds to 1200 seconds; timed-out tasks restart the clock on the next real execution after being revived.
- The real execution state of Workers, the Planner and the main Agent uniformly drives the task status badge, fixing Workers still showing "Idle" while running.
- Worker sessions keep individual controls and the current session's token summary, and the model name moved to the right of the current session title.

#### Removed

- Removed the Worker multi-select, select-all and batch pause/continue UI, and also removed the Worker batch control API and Mock contract.
- Removed the token badge and Tooltip from Worker list rows; the underlying complete token ledger and task aggregation APIs are kept.

### LLM

#### Added

- Tasks support an ordered LLM config chain; when provider quota exhaustion is explicitly identified it automatically switches to the next config, and persists the current config, exhaustion state and error summary.
- Running and paused tasks support editing the full config chain, reordering it and manually switching the current config, effective from the next LLM call.
- Automatic switches, manual switches and whole-chain exhaustion are written as structured system activity, and pop up deduplicatable alerts via the task activity feed.
- Added a task role model resolution API that uniformly resolves the config and model used by the next call of the Main Agent, Planner and Worker.
- Added an optional global LLM Pool, supporting configurable call order, designated model failure fallback, health status, cooldown recovery and manual reset.
- Added an always-on LLM usage ledger, aggregating input, output and cache tokens by task, session, model and config.

#### Changed

- Goal decomposition, the Planner, Workers and the main Agent share a task-level LLM runtime (resolution priority below).
- Failover only responds to explicit quota, balance or billing errors; ordinary rate limiting, auth, network, server-side and context errors don't trigger a wrong switch.
- The LLM config page is now a list of config cards with right-side drawer editing, and provides Pool round-robin order, priority, exclusions, health status and recovery actions.
- Config chain exhaustion info supports wrapping on narrow screens; the current model changed from a session list icon to a text label next to the current session title with a full Tooltip.
- The order in which each role within a task resolves its LLM is now "Agent binding → task config chain → global": a role explicitly bound to a model always runs on that model, only unbound roles fall to the task chain, and when the task chain is empty they fall to global.
- An empty outbound proxy means direct connection, no longer falling back to the `HTTP_PROXY`/`HTTPS_PROXY` environment variables (an explicit `ARTEX_LLM_PROXY` is unaffected); proxy input supports `socks5://user:pass@host:port` with username and password.

#### Fixed

- Transient streaming failures before commit (before any output was produced) are safely retried with backoff on the same provider, significantly reducing cases of "interrupted after running only one or two tools, no summary, final state `model_error`"; quota exhaustion, overlong context and deterministic 4xx errors are not retried and are still handled by failover and compaction recovery respectively.

#### Removed

- Removed the icon-style LLM indicator in the session list, to avoid confusion with Worker status icons.

### UI

#### Added

- Tasks support creating, renaming, deleting and filtering global categories; a new task can pick a category directly.
- Added global task template CRUD and a right-side management drawer; a new task can load a preset description and goals, or save the current content as a template.
- New tasks support linking multiple source tasks and multiple company asset scopes; company scopes are auto-recognized from a single multi-line text box as domains, URLs, IPs, CIDRs, ICP and company keywords.
- Task tested assets support adding and deleting in real time, recording sources such as manual, company, inherited or Agent-discovered; the current tested assets and a source summary are shown next to the Worker session title.
- The findings page groups by task with independent pagination within each group, and supports filling in a description for a vulnerability and creating a high-priority deep-dive intent.
- Conversations support rename, pin, unpin and delete; the task list supports batch pause and continue for the current page.
- Traffic details and tool execution details are now right-side drawers; HTTP messages fill in `Host` and highlight the request line, status code, headers, JSON and markup bodies.
- The task list action column gains "Details" and "Pause/Continue" buttons; the session send key can be configured in system settings (`Enter` / `Cmd+Enter` etc.), and the web search section gains a proxy input box.
- The badge to the right of the session title now shows the LLM config name, with the model ID moved to the hover Tooltip; tasks can have an optional name, and the task list gains a name column and search (falling back to the description if empty).
- The skills page adds usage count, last used time and missing dependency stats, making it easier to locate skills that aren't taking effect or aren't installed.

#### Changed

- Task titles are now focusable details links; task assets, company assets, findings task groups and vulnerabilities within groups use real server-side pagination, stable sorting and exact totals.
- Adding a company now uses a right-side drawer, with scope entry unified as multi-line text with real-time recognition, validation and type preview.
- The main Agent input box supports auto-grow, `Enter` to send, `Shift+Enter` for a newline, and avoids accidental sends during Chinese IME composition.
- Agent preview, task reports and related details uniformly use shared Markdown rendering; delete confirmations, long errors and mobile drawer widths are fixed uniformly.
- The task template picker displays and searches by template name and submits by template ID; restored the system's original font size, and the app version is uniformly shown as `0.3.3`.
- Task totals and current session tokens only highlight the input, cache read and output numbers; the send button uses a simpler up-arrow icon.
- The task overview's test scope shows company names instead of IDs.

#### Fixed

- Plain text containing dots is no longer misjudged as an ICP filing number.
- Fixed names in the variable catalog colliding with global runtime variables (such as `{{.Now}}`), causing the Agent editor variable list to render duplicate keys.

#### Removed

- Removed the task card's separate "Enter" button; clicking the task title uniformly enters details.
- Removed the dashboard "New task" button, the Logo URL input when adding a company, and the vulnerability count Badges at the top of the findings page and in task groups.
- Reverted the style that enlarged the global font by 10%, and removed the Worker checkboxes, model icons and row-level token stats from the session list.

### Agent

#### Added

- New tasks can directly link multiple existing tasks, inheriting in real time and read-only the goals, facts, findings, completed intents, asset scope and blackboard context of the direct source tasks.
- Blackboard read tools support querying source task nodes, facts, findings and execution traces on demand; inherited nodes carry a source marker and all write tools refuse to modify them.
- Company scope tools support domains, URLs, IPs, CIDRs, ICP and company keywords; keywords serve only as Agent scope hints and don't participate in automatic asset attribution.
- The finding deep-dive action creates a high-priority manual Worker intent in the original task with an asset anchor and a `derived_from` edge.
- The overview gains a "Goal management" card, supporting manually viewing, adding, modifying and deleting goals; adding or modifying notifies the Planner and revives the task, while deleting hard-deletes the goal node (cascading to edges and anchors) without reviving.
- The main Agent gets the `steer_work` tool by default, letting it inject course-correction instructions into a running Worker in real time without interrupting it or losing progress (still validating that the intent belongs to this task).

#### Changed

- Source task intents don't enter the new task's frontier; the new task keeps using its own exploration, execution queue, working directory and conversation history.
- Deleting a running intent no longer destroys data: it is instead stopped at `stopped`, a deletion reason is required, and the reason is attached to the intent as a fact and written into the payload; the planner perceives it via the `cancelled` trigger (keeping the intent content and reason).
- Main Agent chat is now driven by the server-side activity feed, and input can be recovered on send failure; opening a session auto-scrolls to the bottom and keeps the last reply visible after details lazy-load.
- Pausing a task terminates the current Main Agent, Planner and Worker calls, but doesn't prevent the user from actively starting a new main Agent orchestration session while paused.
- Cancellation, shutdown and stream interruption uniformly keep the real termination reason, generated content, run rounds, duration, tokens and unreturned tool calls.

#### Removed

- Removed the main Agent client's optimistic message echo, avoiding duplicate messages and cross-session stream mixing under pause, failure or concurrent activity.

### Constraints

#### Added

- The goal decomposition stage first extracts operation constraints (allow/deny) before decomposing goals, and the main Agent can add more at runtime; constraints are injected into the Planner and Worker system prompts with the highest priority, and after debiasing, diversity and surface-broadening exploration explicitly yield to constraints.
- The overview gains a "Constraint management" card and create/update/delete APIs; the constraint injection scope can be toggled separately for Planner / Worker (both on by default, read every round, effective immediately). The new table `task_constraints` uses `CREATE TABLE IF NOT EXISTS`, so old DBs create it automatically.

### Interception

#### Added

- Command interception adds model fallback approval on top of regex/string rules: when no rule matches, the model makes an `ALLOW`/`ASK`/`DENY` semantic verdict, with configurable failure fallback action and approval timeout action; the interception page is split into two tabs, "Interception rules / Model config", and model verdicts are labeled with a `[模型]` prefix and a reason.

#### Fixed

- Hardened judge output parsing, preventing a correct `DENY` verdict from being mistaken for an allow.

### Traffic

#### Changed

- Traffic recording now stores whole exchanges in SQLite (large bodies overflow into a hash-deduplicated blob bucket), and text bodies get a trigram full-text index, supporting arbitrary substring and Chinese search; deletion degrades to a single SQL transaction, dropping from hours to milliseconds without stalling recording in the meantime. Adds streaming download of large bodies and a full-text parameter for `traffic_search`; old file-tree data needs no migration and can still be read, searched and deleted.

### Tasks and assets

#### Added

- Task creation gains an "Asset coverage feature" toggle (on by default): when off, coverage is not computed or shown, the situation graph only shows assets, `task_scope` is not accumulated automatically, and the related tools are removed from the Planner / main Agent; company linking is unaffected by this toggle.
- `insert_assets` gains a per-asset `related` flag (default `true`): only effective when coverage is on; `false` only goes into the shared asset store without counting toward this task's coverage (e.g. sibling sites or unrelated assets discovered incidentally).
- Company scopes and task tested assets uniformly support text recognition of domains, URLs, IPs, CIDRs, ICP and keywords; domains/IPs can create or reuse global assets, while CIDR/ICP/keywords are kept as task scope context.

### Build

- `build.sh --release` supports building Linux amd64/arm64, macOS amd64/arm64 and Windows amd64 in one go, and generates zip release packages with `skills/`, config examples and the README.
- Uses the Go linker to strip debug info and generate zip release packages; UPX is now explicitly optional, avoiding its self-extracting ELF segfaulting at startup in some Linux environments.
- The Release Workflow runs a startup smoke test on the Linux amd64 binary, and provides `SHA256SUMS` with the release packages.

### Contributors

- [@neouks](https://github.com/neouks)

## [0.3.2] - 2026-08-20

### Added

- New tasks support linking multiple existing tasks, inheriting in real time and read-only the facts, findings, completed intents, asset scope and blackboard context of the direct source tasks; the new task still uses its own execution queue, working directory and session history.
- Tasks support an ordered LLM config chain. When provider quota exhaustion is explicitly identified it automatically switches to the next config, and persists the current config, exhaustion state and structured audit activity.
- Running and paused tasks support editing the LLM config chain, reordering it and manually switching the current config; automatic switches, manual switches and whole-chain exhaustion are shown as notices on the task details page.
- Running Workers support pause, resume and cancel. Pause keeps blackboard data, while cancel transactionally cleans up the intent and its directly produced facts, findings and execution records after the Worker stops writing.
- Deleting a task can optionally also clean up linked assets, traffic, findings and task files on disk, with a deletion barrier, concurrency protection and auditable deletion stats added.
- Added an optional task concurrency cap; new tasks that hit the cap queue in FIFO order and start automatically once a run slot is freed.
- Added task asset pagination, company asset pagination, task category stats and a paginated intent Mock contract.
- Added the `build.sh` single-binary build script, supporting static frontend export, resource embedding, cross-platform targets and build version injection.

### Changed

- The task-level explicit LLM config chain coexists with the existing global LLM Pool; the explicit chain keeps strict quota failover semantics, and without an explicit chain the Agent binding and global config rules still apply.
- The main Agent input box supports auto-growing multi-line input, `Enter` to send, `Shift+Enter` for a newline, and avoids accidental sends during Chinese IME composition.
- The Planner, Workers and the main Agent share a task-level LLM runtime; the task chain uses the smallest context window among candidate models as the safe compaction threshold.
- The task details page shows running, idle and paused states based on real LLM call state; source tasks' facts, findings, intents, asset references and graph nodes are uniformly marked with their source and kept read-only.
- Agent preview, task reports and related details views uniformly use the shared Markdown rendering component.
- LLM model configs use card and drawer interaction, and the model list supports independent scrolling inside the drawer.
- Pausing a task no longer blocks main Agent chat: main Agent orchestration sessions are independent of task pause, and new messages can still be sent while paused (pause only terminates the round currently in progress).
- The default wall-clock duration of a single Worker run changed from 600 seconds to 1200 seconds.

### Fixed

- Removed the optimistic echo in the main Agent console, switching to pure server-side data rendering, fixing messages getting jumbled or mixed with other sessions' content in scenarios such as pause or send failure.
- Opening a main Agent session scrolls to the bottom by default and fully shows the last reply: after the last reply's full content lazy-loads and expands, it auto-sticks to the bottom instead of being pushed off screen.
- Fixed the main Agent session continuing to run after pausing a task, and the orchestration Agent not stopping the main Agent in sync when pausing a task.
- Fixed the task status badge and action button states being out of sync when the task completes or the Planner, Worker or main Agent is actually running.
- Fixed possible late blackboard writes or leftover files between Worker cancellation, task deletion and concurrent writes.
- Fixed broken Markdown preview, long text overflowing delete confirmations, mobile widths and some misaligned task details buttons.
- Added named termination reasons to all Agent cancellation paths; activity details can show the canceling party, final state, run rounds, duration, token usage and unreturned tool calls.
- Fixed a race where the parent context could preempt and overwrite the named `shutdown` reason during backend shutdown, and garbled text caused by Chinese activity summaries being truncated by bytes.
- Fixed cancellation events with partial streaming output losing the real termination reason, while keeping the content generated before cancellation.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)

[Unreleased]: https://github.com/Autumn-27/ARTEX/compare/v0.3.10...HEAD
[0.3.10]: https://github.com/Autumn-27/ARTEX/compare/v0.3.9...v0.3.10
[0.3.9]: https://github.com/Autumn-27/ARTEX/compare/v0.3.8...v0.3.9
[0.3.8]: https://github.com/Autumn-27/ARTEX/compare/v0.3.7...v0.3.8
[0.3.7]: https://github.com/Autumn-27/ARTEX/compare/v0.3.6...v0.3.7
[0.3.6]: https://github.com/Autumn-27/ARTEX/compare/v0.3.5...v0.3.6
[0.3.5]: https://github.com/Autumn-27/ARTEX/compare/v0.3.4...v0.3.5
[0.3.4]: https://github.com/Autumn-27/ARTEX/compare/v0.3.3...v0.3.4
[0.3.3]: https://github.com/Autumn-27/ARTEX/compare/v0.3.2...v0.3.3
[0.3.2]: https://github.com/Autumn-27/ARTEX/compare/v0.3.1...v0.3.2
