# Changes to Norma in this copy

This directory is a modified copy of [Norma](https://github.com/Autumn-27/Norma)
**v0.4.3** (`github.com/Autumn-27/norma`), licensed under the Apache License 2.0
(see `LICENSE`, taken from upstream commit `52f17dd5`). It is used by Elenxa
through a `replace` directive in the top-level `go.mod`.

Before any change, this copy was verified byte-identical to the published
v0.4.3 module (go.sum `h1:DnmOhJPL6OZSMnCVYO5Ujh4yVINuDarEickVi9jx1hI=`).
Upstream has no NOTICE file.

As required by Apache-2.0 section 4(b), these are the files changed by the
Elenxa authors (2026-11-17 to 2026-11-18). The upstream `examples/` directory
is not included.

## Security hardening

| File | Change |
|---|---|
| `tool/helpers.go`, `tool/files.go`, `tool/ls.go`, `tool/multiedit.go`, `tool/search.go`, `tool/tool.go`, `harness/query.go`, `agentcore/agentcore.go` | File tools confined to the working directory plus configured `AllowedRoots`; symlinks resolved before the check |
| `tool/ripgrep.go` | ripgrep is only looked up on `PATH`, never downloaded |
| `tool/webfetch.go` | Dial guard: no loopback, link-local or cloud-metadata addresses; at most 5 redirects |
| `tool/websearch.go` | No longer marked read-only (the query leaves the machine), so it is not auto-allowed |
| `tool/childenv.go` (new), `tool/bash.go`, `tool/task.go`, `mcp/stdio.go` | Child processes get an allowlisted environment instead of the full parent environment |
| `tool/shell_tools.go`, `tool/shell_session.go` | `shell_open` / `shell_send` / `shell_close` require approval like Bash; only `shell_read` / `shell_list` are auto-allowed |
| `tool/deferred.go`, `harness/tools.go`, `permission/permission.go` | Permission rules also apply to the inner tool of wrappers such as `ExecuteExtraTool` (`DelegatingTool`, exported `MatchRule`) |
| `permission/permission.go`, `subagent/subagent.go` | The approving permission context travels with each tool call; subagents inherit the parent's deny rules and a mode no wider than the parent's |

## Prompt caching (Anthropic provider)

| File | Change |
|---|---|
| `llm/promptcache.go` (new) | Process-wide prompt-caching switch (default on) |
| `llm/anthropic.go` | System prompt cached even with no dynamic boundary; top-level automatic `cache_control`; nothing cached when the switch is off |

## Translation

Chinese comments, log messages and error strings were translated to English
across the changed files above and in `noa/archive.go`.

## Tests

`harness/settlement_test.go`, `harness/timeout_settle_test.go`,
`noaadapter/longsession_test.go`, `noaadapter/tierchain_test.go`,
`tool/builtins_test.go`, `tool/ripgrep_test.go`, `tool/shell_session_test.go`
and `llm/anthropic_system_test.go` were updated for the changes and
translations above.
