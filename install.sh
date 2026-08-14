#!/usr/bin/env bash
set -e

REPO="halpworld/halpradio"
BIN_NAME="halpradio"

# Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "${OS}" in
  darwin) OS="darwin" ;;
  linux) OS="linux" ;;
  *)
    echo "Error: Unsupported operating system: ${OS}"
    echo "halpradio currently supports macOS (Darwin) and Linux."
    exit 1
    ;;
esac

# Detect Architecture
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "Error: Unsupported architecture: ${ARCH}"
    echo "halpradio supports x86_64/amd64 and arm64/aarch64."
    exit 1
    ;;
esac

# Get latest release tag if not specified
if [ -z "${VERSION}" ]; then
  echo "🔍 Fetching latest halpradio release..."
  VERSION=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
  if [ -z "${VERSION}" ]; then
    VERSION="v0.0.1"
  fi
fi

# Clean version string (e.g. 1.0.0 from v1.0.0)
CLEAN_VERSION="${VERSION#v}"
TARBALL="${BIN_NAME}_${CLEAN_VERSION}_${OS}_${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${TARBALL}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

echo "⬇️  Downloading halpradio ${VERSION} for ${OS}/${ARCH}..."
if ! curl -sSL -f -o "${TMP_DIR}/${TARBALL}" "${DOWNLOAD_URL}"; then
  echo "❌ Error downloading from ${DOWNLOAD_URL}"
  echo "Please verify the release exists or build from source:"
  echo "  go install github.com/${REPO}@latest"
  exit 1
fi

echo "📦 Extracting binary..."
tar -xzf "${TMP_DIR}/${TARBALL}" -C "${TMP_DIR}"

# Determine installation directory
INSTALL_DIR="/usr/local/bin"
if [ ! -w "${INSTALL_DIR}" ]; then
  if command -v sudo >/dev/null 2>&1; then
    USE_SUDO="sudo"
  else
    INSTALL_DIR="${HOME}/.local/bin"
    mkdir -p "${INSTALL_DIR}"
    USE_SUDO=""
  fi
fi

echo "🚀 Installing halpradio to ${INSTALL_DIR}..."
${USE_SUDO} mv "${TMP_DIR}/${BIN_NAME}" "${INSTALL_DIR}/${BIN_NAME}"
${USE_SUDO} chmod +x "${INSTALL_DIR}/${BIN_NAME}"

echo ""
echo "✅ halpradio ${VERSION} installed successfully!"
echo ""
echo "Quick Start:"
echo "  1. Run: halpradio"
echo "  2. Press ? to open WhichKey help"
echo "  3. Press / to search 30,000+ radio stations"
echo ""
