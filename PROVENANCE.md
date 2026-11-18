# Provenance

Elenxa (`github.com/Aporixa/elenxa`) is derived from an imported snapshot of the original ARTEX by Autumn-27 (upstream repo since deleted).

- Upstream version: v0.3.15
- Source commit: e6ec56999175509551a1e43a3ee3b053d0941bd9
- Source tree hash: bcee5171ddb6ce4d8daacbd13b6b36d5f7601b2b
- Obtained via: github.com/RuoJi6/ARTEX-fork-backup (history verified identical to upstream; tree identical to the matching commit in BaaRai/ARTEX)
- Imported: 2026-11-17 with `git archive`; no upstream history is carried.
- License: AGPL-3.0 (see LICENSE). Modifications in this repo remain AGPL-3.0.

Go dependency github.com/Autumn-27/norma v0.4.3 (Apache-2.0) is vendored as a
local, patched copy in `third_party/norma` (`replace` in go.mod). Before
patching, the copy was verified byte-identical to the published module (go.sum
h1 hash `DnmOhJPL6OZSMnCVYO5Ujh4yVINuDarEickVi9jx1hI=`). Its license text is in
`third_party/norma/LICENSE` (upstream commit `52f17dd5`; upstream has no NOTICE
file), and every changed file is listed in `third_party/norma/CHANGES.md`.

The web UI (`web/`) was built by the ARTEX authors on the Studio Admin
Next.js template by Mohammed Arham Khan (MIT; notice kept in `web/LICENSE`).
The template's promotional image (`web/media/`) was removed.

Components written for this repo (no upstream counterpart):
- `cmd/elenxa-browser-mcp` — stdlib-only Go MCP server driving the system
  Chromium over the DevTools pipe; replaces `npx @playwright/mcp`.
- `packaging/chromium-policy.json` — managed Chromium policy that turns off
  sign-in, sync, network time, search/AI prefetch, Safe Browsing, metrics and
  component updates.

All other Go modules are vendored (`vendor/`) and built offline; npm packages
are pinned by `web/package-lock.json` integrity hashes (regenerated without
husky / lint-staged). Base images are pinned by digest in `Dockerfile` and
`docker-compose.yml`.
This repo does not track or pull from any upstream or fork.

## Rename to Elenxa (2026-11-18)

- Go module path `github.com/Autumn-27/artex` changed to `github.com/Aporixa/elenxa`.
- Every `ARTEX` / `artex` identifier was renamed: display name, `ARTEX_*`
  environment variables (now `ELENXA_*`), the binary and `cmd/` paths, the
  `elenxa-browser-mcp` server, compose project/service, database name and user,
  volumes, container user, the `elenxa_token` cookie, browser storage keys and
  the guard message ("[Elenxa Platform Governance · Not a Target Defense]").
  The login username is `Elenxa`.
- The ARTEX logo and screenshots were removed; the UI uses the Aporixa
  open-ouroboros mark.
- The name ARTEX remains only where it attributes the original work (this file,
  the CHANGELOG history, README "Origin" and "License", the login notice).
- The original author's added usage terms (README and login screen) were
  removed. They were further restrictions beyond AGPL-3.0 (section 7). Intended
  use and the disclaimer are in `SECURITY.md`.
