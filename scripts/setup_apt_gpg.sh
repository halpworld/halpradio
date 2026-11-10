#!/usr/bin/env bash
# ==============================================================================
# setup_apt_gpg.sh — Create (or rotate) the APT archive signing key
# ==============================================================================
# Run on a trusted machine, from the repository root. It:
#   1. creates a passphrase-protected, certify-only primary key and a signing
#      subkey that expires (default 2y), in a NEW GnuPG home that is KEPT
#      (default ~/.halpradio-apt-key-<date>) so the primary key and the
#      revocation certificate survive;
#   2. writes the public key to keys/halpradio.pub.asc and
#      keys/halpradio-archive-keyring.gpg (commit these, and update
#      APT_SIGNING_KEY_FPR in .github/workflows/release.yml);
#   3. after you confirm the backup, uploads ONLY the signing subkey
#      (`--export-secret-subkeys`) to the GPG_PRIVATE_KEY Actions secret.
#
# gpg will prompt for the passphrase (pinentry). Back up the GnuPG home and the
# revocation certificate offline before confirming the upload.
#
# Rotation: keep the old public key in keys/halpradio-archive-keyring.gpg for
# one release cycle (concatenate both keys) so existing users keep verifying,
# then remove it.
# ==============================================================================

set -euo pipefail

command -v gpg >/dev/null 2>&1 || { echo "❌ gpg is required but not installed. Install with 'brew install gnupg' or 'apt install gpg'."; exit 1; }
command -v gh >/dev/null 2>&1 || { echo "❌ gh (GitHub CLI) is required but not installed."; exit 1; }
[ -d keys ] || { echo "❌ run from the repository root (keys/ not found)"; exit 1; }

SUBKEY_EXPIRY="${SUBKEY_EXPIRY:-2y}"
UID_STR="halpradio Archive Automatic Signing Key <halpworld@users.noreply.github.com>"
KEY_HOME="${KEY_HOME:-${HOME}/.halpradio-apt-key-$(date +%Y%m%d)}"

if [ -e "${KEY_HOME}" ]; then
  echo "❌ ${KEY_HOME} already exists; set KEY_HOME to a new directory"
  exit 1
fi
mkdir -p "${KEY_HOME}"
chmod 700 "${KEY_HOME}"
export GNUPGHOME="${KEY_HOME}"

# Only the temporary export of the subkey is deleted on exit; the GnuPG home
# (primary key + revocation certificate) is kept on purpose.
EXPORT_FILE="$(mktemp)"
CI_HOME="$(mktemp -d)"
chmod 700 "${CI_HOME}"
cleanup() {
  GNUPGHOME="${CI_HOME}" gpgconf --kill all >/dev/null 2>&1 || true
  rm -rf "${CI_HOME}" "${EXPORT_FILE}"
}
trap cleanup EXIT

echo "🔐 Creating certify-only primary key (you will be asked for a passphrase)..."
gpg --quick-generate-key "${UID_STR}" rsa4096 cert never
FPR="$(gpg --list-secret-keys --with-colons "${UID_STR}" | awk -F: '/^fpr/ { print $10; exit }')"
[[ "${FPR}" =~ ^[0-9A-F]{40}$ ]] || { echo "❌ could not read the new key's fingerprint"; exit 1; }

echo "🔏 Adding signing subkey (expires in ${SUBKEY_EXPIRY})..."
gpg --quick-add-key "${FPR}" rsa4096 sign "${SUBKEY_EXPIRY}"

REVOKE_CERT="${KEY_HOME}/openpgp-revocs.d/${FPR}.rev"
echo "🔑 Primary key fingerprint: ${FPR}"
echo "   Revocation certificate:  ${REVOKE_CERT}"

gpg --armor --export "${FPR}" > keys/halpradio.pub.asc
gpg --export "${FPR}" > keys/halpradio-archive-keyring.gpg
echo "✅ Saved public keys to keys/halpradio.pub.asc and keys/halpradio-archive-keyring.gpg"
echo "   Set APT_SIGNING_KEY_FPR: ${FPR} in .github/workflows/release.yml and in SECURITY.md."

echo ""
echo "Back up ${KEY_HOME} (primary key + revocation certificate) to offline storage now."
read -r -p "Type 'backed up' to upload the signing subkey to GitHub: " answer
if [ "${answer}" != "backed up" ]; then
  echo "Not uploading. To upload later: export with --export-secret-subkeys from ${KEY_HOME},"
  echo "remove the passphrase in a throwaway GNUPGHOME (gpg --passwd), then gh secret set GPG_PRIVATE_KEY."
  exit 0
fi

# Subkeys only: the primary (certify) key never leaves this machine. CI signs
# non-interactively, so the CI copy of the subkey gets no passphrase: it is
# stripped of its passphrase in a throwaway GnuPG home, never in ${KEY_HOME}.
gpg --export-secret-subkeys "${FPR}" | GNUPGHOME="${CI_HOME}" gpg --batch --import
echo "Next: enter the current passphrase, then leave the NEW passphrase EMPTY (CI copy only)."
GNUPGHOME="${CI_HOME}" gpg --passwd "${FPR}"
GNUPGHOME="${CI_HOME}" gpg --armor --export-secret-subkeys "${FPR}" > "${EXPORT_FILE}"
echo "🚀 Uploading GPG_PRIVATE_KEY secret (signing subkey only)..."
gh secret set GPG_PRIVATE_KEY < "${EXPORT_FILE}"

echo "🎉 Done. Keep ${KEY_HOME} offline; it is the only copy of the primary key."
