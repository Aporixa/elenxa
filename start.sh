#!/bin/sh
# Elenxa start script (Linux / macOS / Docker ENTRYPOINT)
#
# Usage:
#   ./start.sh                     run in the foreground (Ctrl-C to stop)
#   ./start.sh -addr 127.0.0.1:9000  extra args are passed through to elenxa
#
# Hardened local build: there is no self-update and no restart-on-exit-75 loop.
# The script simply execs the binary, so elenxa becomes PID 1 inside the container
# and receives SIGTERM from `docker stop` directly (graceful shutdown).
# Restarts on crash are handled by the container/service manager
# (docker compose `restart: unless-stopped`, systemd, ...).
set -eu

cd "$(dirname "$0")"

BIN=./elenxa
[ -x "$BIN" ] || { echo "[elenxa] executable not found: $BIN" >&2; exit 1; }

exec "$BIN" "$@"
