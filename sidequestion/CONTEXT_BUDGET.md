# Long side conversations and the context budget

Fixed on 2026-09-11. The original implementation treated the character count of the request JSON directly as tokens and inherited the main task's 32K output reserve, so with lots of HTML, JS and tool results it rejected normal side questions too early.

## Open-source implementation review

- [Grok CLI side context](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/agent/agent.ts#L739): extracts snippets from recent user and assistant text, with a character budget of about 2000 and at most 400 characters per entry. This path does not maintain a continuous side Q&A history.
- [Grok CLI independent request](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/utils/side-question.ts): independent cancellation signal; output capped at 2048 tokens when the model supports it; no tools provided.
- [Grok CLI main session compaction](https://github.com/superagent-ai/grok-cli/blob/fb97af83f06dca873281d60168430f06c8de6324/src/agent/compaction.ts): estimates tokens, keeps recent content, updates new content into the old summary, and handles truncation across turns.
- [OpenCode session compaction](https://github.com/anomalyco/opencode/blob/b3f1a96c6dd7adeb28b36dd11add1998fc84d67b/packages/core/src/session/compaction.ts): full request estimation, output/buffer reserve, recent content plus a rolling summary, tool-free summary requests; this implementation defaults to a recent budget of 8000 and a summary output cap of 4096 tokens.
- [OpenCode overflow recovery](https://github.com/anomalyco/opencode/blob/b3f1a96c6dd7adeb28b36dd11add1998fc84d67b/packages/core/src/session/runner/llm.ts): attempts overflow recovery only when assistant output has not started yet, and the recovered call does not enter the same overflow recovery path again.

Elenxa borrows the independent output budget, recent content plus rolling summary, and limited recovery approaches. It keeps norma v0.3.6's structured messages and tool pairing rather than copying Grok's text excerpts, and does not write OpenCode-style main session compaction events into the Elenxa main transcript.

## Request budget and execution

- Messages reuse norma's per-content-block UTF-8 byte estimate with a 4/3 margin; system prompts, tool schemas and message envelope overhead are counted on top. The estimate is not an exact model token count.
- Side output defaults to at most 8192 tokens and never exceeds the output cap already set in the main configuration. The service environment variable `ELENXA_BTW_MAX_OUTPUT_TOKENS` can set a cap of 256-32768; it does not change the product's default model or main task parameters.
- The input budget is the context window minus the output cap and a safety margin; an unknown window uses the platform default of 200K. The safety margin is 5% of the window, minimum 128 and maximum 8192 tokens.
- Successful Q&A is loaded in batches of at most 20 pairs by increasing ordinal. At most 20 pairs are kept verbatim, with a token budget of at most 1/4 of the input budget and no more than 16K.
- Excess Q&A is folded into a rolling summary. The summary carries historical source and context time; historical assistant answers are not equivalent to new tool evidence, and on conflict the latest main snapshot takes precedence.
- When the main context is still too long, only the old messages in the copy are summarized, keeping at most 8K tokens of recent content; the cut point never splits a tool call from its result. An oversized single pair goes into the summary as a whole.
- Summary input is split into UTF-8-safe chunks according to the actual remaining window, with an output cap of 2048 tokens; empty summaries, truncation, tool calls or exceeding the summary budget are never cached. A single side question makes at most 12 summary calls and is bound by the same 120-second timeout; hitting the limit fails explicitly rather than looping forever.
- If the model's first response reports a context overflow and no text or tool call has been output yet, it retries at most once after further shrinking; if the estimated size does not drop, recovery stops immediately. Other model errors and partial streaming output do not trigger this recovery.
- All usage obtained, including summaries, failed attempts and usage at cancellation, is added to the same side request. When the Provider returns no usage, only zero can be recorded; estimates must not be passed off as actual usage.

## Persistence and UI

`side_question_sessions.memory` stores the old Q&A summary, the ordinals it covers, and the main context summary cached by snapshot identity. `side_question_requests.context_info` stores the preparation stage, the actual number of replayed pairs, summary usage and the budget estimate.

A summary is saved only while the original request is still running and the clear version matches; clearing also clears the cache, and late writes do not restore cleared data. A new snapshot does not reuse the old snapshot's summary. Summary fields are saved with v3 task archives; restoring an old v3 archive missing these fields fills in empty objects, and v1/v2 stay compatible.

POST first accepts and returns the request; preparation and compaction run in the background without holding the admission lock or a database transaction. SSE/history show the preparing, consolidating Q&A, compacting copy and answering stages; a compaction failure is saved as that side request's failed terminal state. The frontend keeps the error and restores the draft of the failed question, without covering the input box with a floating notification; history polling no longer clears submission errors.

## Validation log

- Replay of 19/20/21/50 pairs, retention of old conclusions across 20 pairs, summary cache reuse after restart: automated tests pass.
- Very long Chinese answers and code context, chunked request budget, tool pairing, unchanged snapshots, cache invalidation on new snapshots: automated tests pass.
- Summary failure/cancellation/truncation/oversize/tool return, clear races, call limit, only one overflow recovery, and no retry on partial streams: automated tests pass.
- Pagination, restart, v1/v2/v3 archives, archive restore of summary cache and budget metadata, old v3 missing new fields, and 20 parent sessions sharing four concurrency slots on an independent PostgreSQL: pass.
- Go candidate service build, frontend TypeScript check, Biome check of modified components, and a Next.js production build in an independent directory: pass.
- The built-in browser used an independent UI fixture at 1280×720 and 390×844 to verify the consolidation stage, summary scope hint, draft restore after failure, no floating error notification, no horizontal overflow and no console errors. The temporary fixture has been removed.
- Read-only replay of existing local Worker snapshots passed the new budget check; for example, Worker #3's 293085-character snapshot is no longer wrongly rejected by local character counting. This item made no external model calls.
- A real conversation test that would send a private Worker snapshot to Grok was rejected by the automatic approval review and was not run; it does not count as a pass.
- 2026-09-11 00:37: restarted the local backend at the user's request, reusing the original database, data directory and login configuration. The running file and candidate binary SHA-256 match; `/api/health` returns OK on both the backend and the frontend proxy.

Validation commands (independent test database only):

```sh
go test -race ./sidequestion ./db ./server -run 'TestSide|TestCheckpoint|TestSnapshot|TestBuildRequest|TestService|TestMainSide|TestTaskArchive' -count=1
go build ./cmd/elenxa
npx tsc --noEmit
npm run build -- --webpack
```

The frontend production build uses an independent copy to avoid overwriting the `.next` of the current preview. The candidate service is at `/private/tmp/elenxa-btw-budget-candidate`, has been copied to `/private/tmp/elenxa-btw-preview/elenxa` and started; the original binary is backed up as `elenxa.before-context-budget` in the same directory.
