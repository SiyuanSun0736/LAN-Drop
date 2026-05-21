#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
PACKAGE_DIR="${ROOT_DIR}/.deploy/linux-amd64/packages"
STAGE_ROOT="$(mktemp -d /tmp/landrop-linux-build.XXXXXX)"
STAGE_REPO="${STAGE_ROOT}/repo"
FRONTEND_STAGE="${STAGE_REPO}/frontend"
BACKEND_STAGE="${STAGE_REPO}/backend"

cleanup() {
  rm -rf "${STAGE_ROOT}"
}

trap cleanup EXIT

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require_command tar
require_command node
require_command npm
require_command npx
require_command go

run_with_retry() {
  local attempts="$1"
  shift

  local attempt
  for ((attempt = 1; attempt <= attempts; attempt++)); do
    if "$@"; then
      return 0
    fi

    if [[ "$attempt" -lt "$attempts" ]]; then
      echo "command failed, retrying (${attempt}/${attempts}): $*" >&2
    fi
  done

  return 1
}

export ELECTRON_MIRROR="${ELECTRON_MIRROR:-https://npmmirror.com/mirrors/electron/}"
export ELECTRON_BUILDER_BINARIES_MIRROR="${ELECTRON_BUILDER_BINARIES_MIRROR:-https://npmmirror.com/mirrors/electron-builder-binaries/}"

rm -rf "${PACKAGE_DIR}"
mkdir -p "${PACKAGE_DIR}" "${STAGE_REPO}"

(
  cd "${ROOT_DIR}"
  tar \
    --exclude='./.deploy' \
    --exclude='./frontend/node_modules' \
    --exclude='./frontend/out' \
    --exclude='./frontend/release' \
    --exclude='./frontend/resources/backend' \
    --exclude='./backend/bin' \
    -cf - .
) | tar -xf - -C "${STAGE_REPO}"

mkdir -p "${FRONTEND_STAGE}/resources/backend"

(
  cd "${BACKEND_STAGE}"
  go mod tidy
  GOOS=linux GOARCH=amd64 go build -buildvcs=false -trimpath -ldflags='-s -w' -o "${FRONTEND_STAGE}/resources/backend/landrop-agent" ./cmd/landrop-agent
)

(
  cd "${FRONTEND_STAGE}"
  npm install
  npm run build
  run_with_retry 3 npx electron-builder --linux AppImage deb --x64
)

cp -a "${FRONTEND_STAGE}/release/." "${PACKAGE_DIR}/"

echo "linux packages copied to ${PACKAGE_DIR}"