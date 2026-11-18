#!/usr/bin/env bash
# Elenxa cross-platform release builder.
#
# The default mode builds one target and embeds the already-exported frontend.
# `./build.sh --release` builds and packages all supported desktop/server targets.
#
# Environment variables:
#   ELENXA_TARGET_OS=linux             One target OS in single-target mode.
#   ELENXA_TARGET_ARCH=amd64           One target arch in single-target mode.
#   ELENXA_TARGETS=linux/amd64,...     Comma-separated targets for multi-target mode.
#   ELENXA_BUILD_VERSION=v0.3.3        Version embedded in the binary and archive name.
#   ELENXA_OUTPUT=/path/to/elenxa       Explicit binary path in single-target mode.
#   ELENXA_OUTPUT_DIR=dist             Directory for default binary paths.
#   ELENXA_PACKAGE=1                   Create a zip archive for each target.
#   ELENXA_PACKAGE_DIR=dist            Directory for release archives.
#   ELENXA_COMPRESS=off                UPX mode: off, auto, or required.
#   ELENXA_UPX_ARGS="--best --lzma"    Arguments passed to UPX.
#   ELENXA_SKIP_FRONTEND=1             Reuse server/webui/dist (for CI artifact builds).
#   ELENXA_SKIP_NPM_CI=1               Skip npm ci while rebuilding the frontend.
#   ELENXA_GOSUMDB=off                 Go checksum database (off: offline vendored build).
#
# Hardened local build: Go builds are fully offline (GOFLAGS=-mod=vendor,
# GOPROXY=off, GOTOOLCHAIN=local; run `go mod vendor` first), npm ci runs with
# --ignore-scripts, and Next.js telemetry is disabled.
set -euo pipefail

cd "$(cd "$(dirname "$0")" && pwd)"

info() { printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok() { printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn() { printf '\033[33m[!]\033[0m %s\n' "$*" >&2; }
die() { printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Usage:
  ./build.sh                         build for the current OS and architecture
  ./build.sh --target linux/amd64   build one specified target
  ./build.sh --release               build and package all supported targets

Options:
  --release              build Linux, macOS and Windows amd64/arm64 targets and produce zips
  --target OS/ARCH       set a single target, e.g. windows/amd64
  --upx                  force UPX compression of the binary (may affect compatibility on some Linux environments)
  --no-compress          do not use UPX; only use Go linker stripping and zip compression
  --help                 show help

The multi-target list can be overridden with ELENXA_TARGETS, e.g.:
  ELENXA_TARGETS=linux/amd64,windows/amd64 ./build.sh --release
EOF
}

RELEASE_TARGETS_DEFAULT="linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64"
ELENXA_RELEASE="${ELENXA_RELEASE:-0}"
ELENXA_COMPRESS="${ELENXA_COMPRESS:-off}"
ELENXA_PACKAGE="${ELENXA_PACKAGE:-0}"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --release)
      ELENXA_RELEASE=1
      ELENXA_PACKAGE=1
      shift
      ;;
    --target)
      [ "$#" -ge 2 ] || die "--target requires an OS/ARCH argument"
      target_arg="$2"
      case "$target_arg" in
        */*)
          ELENXA_TARGET_OS="${target_arg%%/*}"
          ELENXA_TARGET_ARCH="${target_arg##*/}"
          ELENXA_TARGETS="$target_arg"
          ;;
        *) die "target must be OS/ARCH, e.g. linux/amd64" ;;
      esac
      shift 2
      ;;
    --no-compress)
      ELENXA_COMPRESS=0
      shift
      ;;
    --upx)
      ELENXA_COMPRESS=required
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *) die "unknown argument: $1 (use --help for usage)" ;;
  esac
done

command -v go >/dev/null 2>&1 || die "Go not found (the project requires Go 1.26 or later)"
[ -d vendor ] || die "vendor/ does not exist: run go mod vendor first (offline builds do not access GOPROXY)"

export GOFLAGS=-mod=vendor
export GOPROXY=off
export GOTOOLCHAIN=local
export NEXT_TELEMETRY_DISABLED=1

ELENXA_GOSUMDB="${ELENXA_GOSUMDB:-off}"
if [ -z "${ELENXA_BUILD_VERSION:-}" ]; then
  if command -v git >/dev/null 2>&1 && git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    ELENXA_BUILD_VERSION="$(git describe --tags --always --dirty)"
  else
    ELENXA_BUILD_VERSION="dev"
  fi
fi
# Release tags are commonly passed as v0.3.3; keep the binary version consistent.
ELENXA_BUILD_VERSION="${ELENXA_BUILD_VERSION#v}"
ELENXA_OUTPUT_DIR="${ELENXA_OUTPUT_DIR:-dist}"
ELENXA_PACKAGE_DIR="${ELENXA_PACKAGE_DIR:-$ELENXA_OUTPUT_DIR}"
ELENXA_UPX_ARGS="${ELENXA_UPX_ARGS:---best --lzma}"

if [ "${ELENXA_RELEASE}" = "1" ]; then
  ELENXA_TARGETS="${ELENXA_TARGETS:-$RELEASE_TARGETS_DEFAULT}"
else
  ELENXA_TARGET_OS="${ELENXA_TARGET_OS:-$(GOSUMDB="$ELENXA_GOSUMDB" go env GOOS)}"
  ELENXA_TARGET_ARCH="${ELENXA_TARGET_ARCH:-$(GOSUMDB="$ELENXA_GOSUMDB" go env GOARCH)}"
  ELENXA_TARGETS="${ELENXA_TARGETS:-${ELENXA_TARGET_OS}/${ELENXA_TARGET_ARCH}}"
fi

if [ "${ELENXA_SKIP_FRONTEND:-0}" = "1" ]; then
  [ -d server/webui/dist ] || die "ELENXA_SKIP_FRONTEND=1 but server/webui/dist does not exist"
else
  command -v npm >/dev/null 2>&1 || die "npm not found (the static frontend build requires Node.js/npm)"
  command -v rsync >/dev/null 2>&1 || die "rsync not found"
  info "Building frontend static assets"
  if [ "${ELENXA_SKIP_NPM_CI:-0}" != "1" ]; then
    (cd web && npm ci --ignore-scripts)
  fi
  (cd web && npm run build:static)
  info "Syncing frontend assets to server/webui/dist"
  mkdir -p server/webui/dist
  rsync -a --delete web/out/ server/webui/dist/
fi

compress_binary() {
  binary="$1"
  goos="$2"
  case "$ELENXA_COMPRESS" in
    0|off|false|none)
      info "Skipping UPX: $binary"
      return 0
      ;;
    auto|required|true|1) ;;
    *) die "ELENXA_COMPRESS must be off, auto or required" ;;
  esac

  if ! command -v upx >/dev/null 2>&1; then
    if [ "$ELENXA_COMPRESS" = "required" ]; then
      die "ELENXA_COMPRESS=required but upx was not found"
    fi
    warn "upx not found; keeping the linker-compressed result: $binary"
    return 0
  fi

  before=$(wc -c < "$binary" | tr -d ' ')
  upx_args="$ELENXA_UPX_ARGS"
  [ "$goos" = "darwin" ] && upx_args="$upx_args --force-macos"
  # shellcheck disable=SC2086
  if ! upx $upx_args -- "$binary"; then
    if [ "$ELENXA_COMPRESS" = "required" ]; then
      die "UPX compression failed: $binary"
    fi
    warn "UPX does not support this target format; keeping the uncompressed binary: $binary"
    return 0
  fi
  after=$(wc -c < "$binary" | tr -d ' ')
  ok "UPX compression done: $binary (${before} -> ${after} bytes)"
}

package_binary() {
  binary="$1"
  goos="$2"
  goarch="$3"
  package_name="elenxa-${ELENXA_BUILD_VERSION}-${goos}-${goarch}"
  package_root="${ELENXA_PACKAGE_DIR}/${package_name}"
  archive="${ELENXA_PACKAGE_DIR}/${package_name}.zip"

  command -v zip >/dev/null 2>&1 || die "packaging requires zip"
  rm -rf "$package_root" "$archive"
  mkdir -p "$package_root"
  cp "$binary" "$package_root/"
  browser_bin="$(dirname "$binary")/elenxa-browser-mcp"
  [ "$goos" = "windows" ] && browser_bin="${browser_bin}.exe"
  if [ -f "$browser_bin" ]; then cp "$browser_bin" "$package_root/"; fi
  # Launch script (only the one for the target OS is included).
  if [ "$goos" = "windows" ]; then
    cp start.bat "$package_root/"
  else
    cp start.sh "$package_root/"
    chmod +x "$package_root/start.sh"
  fi
  cp -R skills "$package_root/"
  cp config.example.json "$package_root/"
  if [ -f README.md ]; then cp README.md "$package_root/"; fi
  (cd "$ELENXA_PACKAGE_DIR" && zip -q -r -9 "$(basename "$archive")" "$(basename "$package_root")")
  rm -rf "$package_root"
  ok "Release archive: $archive"
}

build_target() {
  target="$1"
  case "$target" in
    */*) ;;
    *) die "invalid target: $target (must be OS/ARCH)" ;;
  esac
  goos="${target%%/*}"
  goarch="${target##*/}"
  case "$goos" in
    linux|darwin|windows) ;;
    *) die "unsupported OS: $goos (supported: linux, darwin, windows)" ;;
  esac

  binary_name="elenxa"
  [ "$goos" = "windows" ] && binary_name="elenxa.exe"
  if [ -n "${ELENXA_OUTPUT:-}" ] && [ "$ELENXA_RELEASE" != "1" ]; then
    output="$ELENXA_OUTPUT"
  else
    output="${ELENXA_OUTPUT_DIR}/elenxa-${goos}-${goarch}/${binary_name}"
  fi
  mkdir -p "$(dirname "$output")"

  info "Building ${goos}/${goarch}, version ${ELENXA_BUILD_VERSION}"
  GOSUMDB="$ELENXA_GOSUMDB" \
  CGO_ENABLED=0 \
  GOOS="$goos" \
  GOARCH="$goarch" \
  go build \
    -tags embedui \
    -trimpath \
    -ldflags "-s -w -buildid= -X main.version=${ELENXA_BUILD_VERSION}" \
    -o "$output" \
    ./cmd/elenxa

  browser_output="$(dirname "$output")/elenxa-browser-mcp"
  [ "$goos" = "windows" ] && browser_output="${browser_output}.exe"
  info "Building elenxa-browser-mcp ${goos}/${goarch}"
  GOSUMDB="$ELENXA_GOSUMDB" \
  CGO_ENABLED=0 \
  GOOS="$goos" \
  GOARCH="$goarch" \
  go build \
    -trimpath \
    -ldflags "-s -w -buildid=" \
    -o "$browser_output" \
    ./cmd/elenxa-browser-mcp

  compress_binary "$output" "$goos"
  if command -v file >/dev/null 2>&1; then file "$output"; fi
  if [ "$ELENXA_PACKAGE" = "1" ]; then package_binary "$output" "$goos" "$goarch"; fi
  ok "Build done: $output"
}

write_checksums() {
  [ "$ELENXA_PACKAGE" = "1" ] || return 0
  checksum_file="$ELENXA_PACKAGE_DIR/SHA256SUMS"
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$ELENXA_PACKAGE_DIR" && for archive in *.zip; do sha256sum "$archive"; done > "$(basename "$checksum_file")")
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$ELENXA_PACKAGE_DIR" && for archive in *.zip; do shasum -a 256 "$archive"; done > "$(basename "$checksum_file")")
  else
    warn "sha256sum or shasum not found; skipping SHA256SUMS"
    return 0
  fi
  ok "Checksum file: $checksum_file"
}

mkdir -p "$ELENXA_OUTPUT_DIR"
if [ "$ELENXA_PACKAGE" = "1" ]; then mkdir -p "$ELENXA_PACKAGE_DIR"; fi

old_ifs="$IFS"
IFS=','
read -r -a targets <<< "$ELENXA_TARGETS"
IFS="$old_ifs"
[ "${#targets[@]}" -gt 0 ] || die "ELENXA_TARGETS must not be empty"
for target in "${targets[@]}"; do
  target="${target//[[:space:]]/}"
  [ -n "$target" ] || continue
  build_target "$target"
done

if [ "$ELENXA_PACKAGE" = "1" ]; then
  write_checksums
  info "Release packages generated in: $ELENXA_PACKAGE_DIR"
fi
