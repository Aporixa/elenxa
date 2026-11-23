# Elenxa — hardened local build.
#
# (No `# syntax=` directive on purpose: it would make BuildKit pull the
#  docker/dockerfile frontend image from Docker Hub. The built-in frontend is used.)
#
# Everything is built inside this Dockerfile from the checked-out sources:
#   - no prebuilt binaries, no registry image of Elenxa itself
#   - base images are pinned by digest (resolved 2026-11-17; bump deliberately)
#   - Go builds offline from vendor/ (run `go mod vendor` before `docker build`)
#   - npm installs strictly from package-lock.json with --ignore-scripts
#   - no NodeSource, no curl|bash, no npm -g, no Playwright downloads
#
# Network access during the build is limited to: the base image pulls (by
# digest), npm registry tarballs pinned by package-lock.json integrity hashes,
# Debian apt packages and pinned Python wheels for the runtime stage.

# ── Stage 1: web UI (Next.js static export) ─────────────────────────────────
FROM node:22-bookworm-slim@sha256:e9f565d32859f0cbfb52125eafe8be39390f24dac302dc3cdf566e8ba3ab21ee AS web
ENV NEXT_TELEMETRY_DISABLED=1 \
    npm_config_fund=false \
    npm_config_audit=false \
    npm_config_update_notifier=false
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts
COPY web/ ./
# build:static = NEXT_EXPORT=1 next build → /web/out
RUN npm run build:static

# ── Stage 2: Go binaries ────────────────────────────────────────────────────
FROM golang:1.26-bookworm@sha256:dc40a13b61304647ed7e5d581d246b86413dfc29d63c6d018310feb93cd98fc0 AS go
ENV CGO_ENABLED=0 \
    GOFLAGS=-mod=vendor \
    GOPROXY=off \
    GONOSUMDB= \
    GOSUMDB=off \
    GOTOOLCHAIN=local
WORKDIR /src
# Whole repo incl. vendor/ and third_party/ (go.mod replaces norma with ./third_party/norma).
COPY . .
# server/webui_embed.go embeds `all:webui/dist` under the `embedui` build tag.
RUN rm -rf server/webui/dist && mkdir -p server/webui
COPY --from=web /web/out /src/server/webui/dist
RUN test -d vendor || { echo "vendor/ missing: run 'go mod vendor' before docker build" >&2; exit 1; } \
 && go build -tags embedui -trimpath -ldflags="-s -w" -o /out/elenxa ./cmd/elenxa \
 && go build -trimpath -ldflags="-s -w" -o /out/elenxa-browser-mcp ./cmd/elenxa-browser-mcp

# ── Stage 3: runtime ────────────────────────────────────────────────────────
FROM python:3.12-slim-bookworm@sha256:935c3c5b12357a7e10caa9c1e6ca5692344d97cf229832ef584be2cec9fc03df
# Tools the agents use. chromium is driven by elenxa-browser-mcp over the
# DevTools pipe (system Chromium, no Playwright browser download).
# libcap2-bin is only needed for the setcap call below.
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates ripgrep curl wget vim-tiny git jq unzip \
      dnsutils iputils-ping netcat-openbsd inetutils-telnet whois nmap \
      nodejs openssl file procps socat xmlstarlet \
      ffuf sqlmap whatweb \
      chromium fonts-liberation libcap2-bin \
    && rm -rf /var/lib/apt/lists/*

# Python's standard library remains the default for small probes, but workers
# routinely need HTTP and HTML/XML parsing for bounded lab scripts. Keep this
# list short and pinned; larger scanners belong in the Debian package layer or
# an explicitly configured MCP/custom tool, not an unbounded Kali-style image.
COPY packaging/runtime-requirements.txt /tmp/runtime-requirements.txt
RUN python -m pip install --no-cache-dir --disable-pip-version-check \
      -r /tmp/runtime-requirements.txt \
 && rm /tmp/runtime-requirements.txt \
 && python -c "import bs4, lxml, requests" \
 && command -v node ffuf sqlmap whatweb >/dev/null

# Raw-socket tools as a non-root user:
#   - ping: Docker sets net.ipv4.ping_group_range so unprivileged ICMP sockets
#     work without extra capabilities.
#   - nmap: SYN/OS/ICMP scans need CAP_NET_RAW. We grant it as a file capability
#     and tell nmap to use it (NMAP_PRIVILEGED=1). The container must keep
#     NET_RAW in its capability set (Docker's default; docker-compose.yml also
#     adds it explicitly). If you run with `cap_drop: [ALL]` without NET_RAW,
#     exec of nmap fails — drop the setcap line and NMAP_PRIVILEGED in that case
#     (nmap then falls back to unprivileged TCP connect scans).
RUN setcap cap_net_raw+eip "$(readlink -f /usr/bin/nmap)"

# Managed Chromium policy: no sign-in, time, search/AI prefetch, safe browsing,
# metrics or update calls to Google. elenxa-browser-mcp also blocks Google service
# domains at the browser's resolver (see cmd/elenxa-browser-mcp/browser.go).
COPY packaging/chromium-policy.json /etc/chromium/policies/managed/elenxa.json
ENV NMAP_PRIVILEGED=1

RUN groupadd --system --gid 10001 elenxa \
 && useradd --system --uid 10001 --gid 10001 --home-dir /app --shell /usr/sbin/nologin elenxa \
 && mkdir -p /app/data /app/keys \
 && chown -R elenxa:elenxa /app/data /app/keys \
 && chmod 0700 /app/keys

WORKDIR /app
COPY --from=go /out/elenxa /app/elenxa
COPY --from=go /out/elenxa-browser-mcp /app/elenxa-browser-mcp
COPY start.sh /app/start.sh
COPY skills/ /app/skills/
RUN chmod 0755 /app/elenxa /app/elenxa-browser-mcp /app/start.sh

USER elenxa
# data/ is the agents' workspace (browsable from the UI); keys/ holds jwt.key and
# the first-run setup-token and is never exposed through the file manager.
ENV ELENXA_KEY_DIR=/app/keys
VOLUME ["/app/data", "/app/keys"]
EXPOSE 8787 8788
# start.sh execs elenxa, so it is PID 1 and receives SIGTERM from `docker stop`.
ENTRYPOINT ["/app/start.sh"]
# 0.0.0.0 inside the container only; docker-compose.yml publishes the ports on
# 127.0.0.1 on the host.
CMD ["-addr", "0.0.0.0:8787", "-proxy", "0.0.0.0:8788"]
