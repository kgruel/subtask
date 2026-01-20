#!/usr/bin/env bash
set -euo pipefail

REPO="zippoxer/subtask"

usage() {
  cat <<'EOF'
Subtask installer (macOS/Linux)

Usage:
  install.sh [--bin-dir DIR] [--version TAG]

Options:
  -b, --bin-dir   Install directory (default: ~/.local/bin)
  -v, --version   Git tag to install (default: latest release)

Examples:
  curl -fsSL https://raw.githubusercontent.com/zippoxer/subtask/main/install.sh | bash
  curl -fsSL https://raw.githubusercontent.com/zippoxer/subtask/main/install.sh | bash -s -- -b ~/.local/bin -v v1.2.3
EOF
}

BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
TAG="${SUBTASK_VERSION:-}"

while [[ $# -gt 0 ]]; do
  case "${1}" in
    -b|--bin-dir)
      BIN_DIR="${2:-}"
      shift 2
      ;;
    -v|--version)
      TAG="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: ${1}" >&2
      usage >&2
      exit 2
      ;;
  esac
done

uname_s="$(uname -s)"
uname_m="$(uname -m)"

case "${uname_s}" in
  Darwin) OS="darwin" ;;
  Linux) OS="linux" ;;
  *)
    echo "Unsupported OS: ${uname_s}" >&2
    exit 1
    ;;
esac

case "${uname_m}" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: ${uname_m}" >&2
    exit 1
    ;;
esac

api_url="https://api.github.com/repos/${REPO}/releases/latest"
if [[ -n "${TAG}" ]]; then
  if [[ "${TAG}" != v* ]]; then
    TAG="v${TAG}"
  fi
  api_url="https://api.github.com/repos/${REPO}/releases/tags/${TAG}"
fi

json="$(curl -fsSL \
  -H "Accept: application/vnd.github+json" \
  -H "User-Agent: subtask-install" \
  "${api_url}")"

tag_name="$(printf '%s' "${json}" | tr -d '\r' | grep -Eo '"tag_name"[[:space:]]*:[[:space:]]*"[^"]+"' | head -n1 | cut -d'"' -f4)"
if [[ -z "${tag_name}" ]]; then
  echo "Failed to determine release tag from GitHub API response." >&2
  exit 1
fi

asset_url="$(printf '%s' "${json}" | tr -d '\r' \
  | grep -Eo '"browser_download_url"[[:space:]]*:[[:space:]]*"[^"]+"' \
  | cut -d'"' -f4 \
  | grep -E "/download/${tag_name}/.*_${OS}_${ARCH}\\.tar\\.gz$" \
  | head -n1)"

checksums_url="$(printf '%s' "${json}" | tr -d '\r' \
  | grep -Eo '"browser_download_url"[[:space:]]*:[[:space:]]*"[^"]+"' \
  | cut -d'"' -f4 \
  | grep -E "/download/${tag_name}/checksums\\.txt$" \
  | head -n1)"

if [[ -z "${asset_url}" ]]; then
  echo "Failed to find a release asset for ${OS}/${ARCH} in ${tag_name}." >&2
  exit 1
fi
if [[ -z "${checksums_url}" ]]; then
  echo "Failed to find checksums.txt in ${tag_name}." >&2
  exit 1
fi

tmp="$(mktemp -d)"
cleanup() { rm -rf "${tmp}"; }
trap cleanup EXIT

asset_name="$(basename "${asset_url}")"

echo "Downloading subtask ${tag_name} (${OS}/${ARCH})..."
curl -fL "${asset_url}" -o "${tmp}/${asset_name}"
curl -fsSL "${checksums_url}" -o "${tmp}/checksums.txt"

expected_sha="$(grep -E "[[:space:]]${asset_name}$" "${tmp}/checksums.txt" | head -n1 | awk '{print $1}')"
if [[ -z "${expected_sha}" ]]; then
  echo "Failed to find checksum for ${asset_name} in checksums.txt." >&2
  exit 1
fi

sha256() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    echo "Neither shasum nor sha256sum found for checksum verification." >&2
    exit 1
  fi
}

actual_sha="$(sha256 "${tmp}/${asset_name}")"
if [[ "${actual_sha}" != "${expected_sha}" ]]; then
  echo "Checksum mismatch for ${asset_name}." >&2
  echo "Expected: ${expected_sha}" >&2
  echo "Actual:   ${actual_sha}" >&2
  exit 1
fi

mkdir -p "${tmp}/extract"
tar -xzf "${tmp}/${asset_name}" -C "${tmp}/extract"

bin_path="$(find "${tmp}/extract" -maxdepth 2 -type f -name subtask -print | head -n1)"
if [[ -z "${bin_path}" ]]; then
  echo "Failed to find subtask binary in archive." >&2
  exit 1
fi

mkdir -p "${BIN_DIR}"
install -m 0755 "${bin_path}" "${BIN_DIR}/subtask"

echo "Installed subtask to ${BIN_DIR}/subtask"
if [[ ":${PATH}:" != *":${BIN_DIR}:"* ]]; then
  echo "Note: ${BIN_DIR} is not on your PATH. Add this to your shell profile:"
  echo "  export PATH=\"${BIN_DIR}:\$PATH\""
fi

