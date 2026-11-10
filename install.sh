#!/usr/bin/env bash
#
# halpradio installer for macOS and Linux.
#
#   curl --proto '=https' --tlsv1.2 -fsSL <url-of-this-file> | bash
#
# What it does:
#   1. Resolves the release to install (latest, or $VERSION such as v0.8.2).
#   2. Downloads the archive for this OS/arch and the release's checksums.txt.
#   3. If cosign is installed, verifies the Sigstore signature on checksums.txt
#      (it must have been produced by this repository's release workflow for
#      that exact tag). A failed verification is always fatal. A missing or
#      unfetchable signature is fatal for every release after v0.8.1 (the last
#      release made before signing existed) unless HALPRADIO_ALLOW_UNSIGNED=1.
#      Without cosign it says the signature was NOT checked.
#   4. Verifies the archive's SHA-256 against checksums.txt and stops on any
#      mismatch.
#   5. Installs only the `halpradio` binary into $INSTALL_DIR
#      (default /usr/local/bin, falling back to ~/.local/bin).
#
# Environment:
#   VERSION                       release tag to install (vMAJOR.MINOR.PATCH)
#   INSTALL_DIR                   target directory
#   HALPRADIO_REQUIRE_SIGNATURE=1 fail unless the signature was verified
#                                 (also when cosign is missing or the release
#                                 predates signing); overrides the next one
#   HALPRADIO_ALLOW_UNSIGNED=1    with cosign installed, continue with a warning
#                                 when a release after v0.8.1 has no signature
#                                 (a signature that fails to verify still stops)
#
# The whole script is inside main(), called on the last line, so a download
# that is cut short executes nothing.

set -euo pipefail

REPO="halpworld/halpradio"
BIN_NAME="halpradio"
VERSION_RE='^v[0-9]+\.[0-9]+\.[0-9]+$'
# Last release published before checksums.txt was signed. Never change this to
# a later version: releases after it must carry checksums.txt.sigstore.json.
LAST_UNSIGNED_RELEASE="v0.8.1"

die() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

fetch() {
  curl --proto '=https' --tlsv1.2 -fsSL --retry 3 "$@"
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    die "neither sha256sum nor shasum is available; cannot verify the download"
  fi
}

detect_os() {
  local os
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "${os}" in
    darwin) printf 'darwin' ;;
    linux) printf 'linux' ;;
    *) die "unsupported operating system: ${os} (halpradio's installer supports macOS and Linux)" ;;
  esac
}

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "${arch}" in
    x86_64 | amd64) printf 'amd64' ;;
    arm64 | aarch64) printf 'arm64' ;;
    *) die "unsupported architecture: ${arch} (supported: x86_64/amd64, arm64/aarch64)" ;;
  esac
}

# version_gt A B: true when vX.Y.Z A is newer than vX.Y.Z B (both validated
# against VERSION_RE; numeric comparison, no sort -V).
version_gt() {
  local a b i
  IFS=. read -r -a a <<<"${1#v}"
  IFS=. read -r -a b <<<"${2#v}"
  for i in 0 1 2; do
    if ((10#${a[i]} > 10#${b[i]})); then return 0; fi
    if ((10#${a[i]} < 10#${b[i]})); then return 1; fi
  done
  return 1
}

resolve_latest() {
  # Follow the /releases/latest redirect instead of calling the rate-limited
  # API; the final URL ends in /tag/<version>.
  local url
  url="$(fetch -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")" ||
    die "could not reach GitHub to find the latest release; set VERSION=vX.Y.Z to choose one"
  printf '%s' "${url##*/}"
}

main() {
  command -v curl >/dev/null 2>&1 || die "curl is required"
  command -v tar >/dev/null 2>&1 || die "tar is required"

  local os arch version clean_version tarball base_url tmp_dir
  local expected actual sig_status install_dir use_sudo

  os="$(detect_os)"
  arch="$(detect_arch)"

  version="${VERSION:-}"
  if [ -z "${version}" ]; then
    echo "Finding the latest halpradio release..."
    version="$(resolve_latest)"
  fi
  if ! [[ "${version}" =~ ${VERSION_RE} ]]; then
    die "invalid release version '${version}' (expected vMAJOR.MINOR.PATCH)"
  fi
  if ! [[ "${arch}" =~ ^(amd64|arm64)$ ]] || ! [[ "${os}" =~ ^(darwin|linux)$ ]]; then
    die "internal error: unexpected platform ${os}/${arch}"
  fi

  clean_version="${version#v}"
  tarball="${BIN_NAME}_${clean_version}_${os}_${arch}.tar.gz"
  base_url="https://github.com/${REPO}/releases/download/${version}"

  tmp_dir="$(mktemp -d)"
  # shellcheck disable=SC2064 # expand tmp_dir now, on purpose
  trap "rm -rf '${tmp_dir}'" EXIT

  echo "Downloading halpradio ${version} for ${os}/${arch}..."
  fetch -o "${tmp_dir}/${tarball}" "${base_url}/${tarball}" ||
    die "download failed: ${base_url}/${tarball}"
  fetch -o "${tmp_dir}/checksums.txt" "${base_url}/checksums.txt" ||
    die "could not download checksums.txt for ${version}; refusing to install unverified files"

  # Signature over checksums.txt (published for releases after v0.8.1).
  sig_status="NOT verified (cosign is not installed)"
  if command -v cosign >/dev/null 2>&1; then
    if fetch -o "${tmp_dir}/checksums.txt.sigstore.json" "${base_url}/checksums.txt.sigstore.json"; then
      # A bundle that does not verify is always fatal, whatever the flags say.
      cosign verify-blob \
        --bundle "${tmp_dir}/checksums.txt.sigstore.json" \
        --certificate-identity "https://github.com/${REPO}/.github/workflows/release.yml@refs/tags/${version}" \
        --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
        "${tmp_dir}/checksums.txt" >/dev/null 2>&1 ||
        die "the signature on checksums.txt for ${version} did NOT verify; not installing"
      sig_status="verified with cosign (signed by ${REPO} release workflow, tag ${version})"
    elif ! version_gt "${version}" "${LAST_UNSIGNED_RELEASE}"; then
      sig_status="NOT verified (release ${version} predates signed releases; checksum only)"
    elif [ "${HALPRADIO_ALLOW_UNSIGNED:-0}" = "1" ] && [ "${HALPRADIO_REQUIRE_SIGNATURE:-0}" != "1" ]; then
      echo "WARNING: release ${version} has no usable checksums.txt.sigstore.json, but every release" >&2
      echo "WARNING: after ${LAST_UNSIGNED_RELEASE} is signed. Continuing only because HALPRADIO_ALLOW_UNSIGNED=1." >&2
      sig_status="NOT verified (signature missing; allowed by HALPRADIO_ALLOW_UNSIGNED=1)"
    else
      die "release ${version} has no usable checksums.txt.sigstore.json although releases after ${LAST_UNSIGNED_RELEASE} are signed; not installing (set HALPRADIO_ALLOW_UNSIGNED=1 to override)"
    fi
  fi
  if [ "${HALPRADIO_REQUIRE_SIGNATURE:-0}" = "1" ] && [[ "${sig_status}" != verified* ]]; then
    die "signature ${sig_status}, and HALPRADIO_REQUIRE_SIGNATURE=1"
  fi

  expected="$(awk -v f="${tarball}" '$2 == f || $2 == "*" f { print $1 }' "${tmp_dir}/checksums.txt")"
  if ! [[ "${expected}" =~ ^[0-9a-f]{64}$ ]]; then
    die "checksums.txt has no valid entry for ${tarball}; not installing"
  fi
  actual="$(sha256_of "${tmp_dir}/${tarball}")"
  if [ "${actual}" != "${expected}" ]; then
    die "SHA-256 mismatch for ${tarball} (expected ${expected}, got ${actual}); not installing"
  fi

  echo "Extracting binary..."
  tar -xzf "${tmp_dir}/${tarball}" -C "${tmp_dir}" "${BIN_NAME}" ||
    die "archive does not contain ${BIN_NAME}"
  if [ ! -f "${tmp_dir}/${BIN_NAME}" ] || [ -L "${tmp_dir}/${BIN_NAME}" ]; then
    die "unexpected ${BIN_NAME} entry in archive"
  fi

  install_dir="${INSTALL_DIR:-/usr/local/bin}"
  use_sudo=""
  if [ ! -w "${install_dir}" ]; then
    if [ -t 0 ] && command -v sudo >/dev/null 2>&1; then
      use_sudo="sudo"
    else
      install_dir="${HOME}/.local/bin"
      mkdir -p "${install_dir}"
    fi
  fi

  echo "Installing halpradio to ${install_dir}..."
  if [ -n "${use_sudo}" ]; then
    sudo install -m 0755 "${tmp_dir}/${BIN_NAME}" "${install_dir}/${BIN_NAME}"
  else
    install -m 0755 "${tmp_dir}/${BIN_NAME}" "${install_dir}/${BIN_NAME}"
  fi

  echo ""
  echo "halpradio ${version} installed to ${install_dir}/${BIN_NAME}"
  echo "  SHA-256 checksum: verified against the release's checksums.txt"
  echo "  Signature:        ${sig_status}"
  if [[ "${sig_status}" != verified* ]]; then
    echo "  (A checksum from the same release detects corrupted downloads, not a"
    echo "   tampered release. Install cosign to also verify the signature.)"
  fi
  case ":${PATH}:" in
    *":${install_dir}:"*) ;;
    *) echo "  Note: ${install_dir} is not on your PATH." ;;
  esac
  echo ""
  echo "Quick Start:"
  echo "  1. Run: halpradio"
  echo "  2. Press ? to open WhichKey help"
  echo "  3. Press / to search 30,000+ radio stations"
}

main "$@"
