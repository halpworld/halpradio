#!/usr/bin/env bash
# ==============================================================================
# setup_apt_gpg.sh — Generate APT GPG Signing Key and Store in GitHub Secrets
# ==============================================================================
# Generates an RSA 4096-bit OpenPGP keypair, saves the public key into keys/,
# and uploads the private key into GitHub Actions secret 'GPG_PRIVATE_KEY'.
# ==============================================================================

set -euo pipefail

command -v gpg >/dev/null 2>&1 || { echo "❌ gpg is required but not installed. Install with 'brew install gnupg' or 'apt install gpg'."; exit 1; }
command -v gh >/dev/null 2>&1 || { echo "❌ gh (GitHub CLI) is required but not installed."; exit 1; }

echo "🔐 Generating halpradio APT repository GPG signing keypair..."

mkdir -p keys
TMP_DIR=$(mktemp -d)
GNUPGHOME="$TMP_DIR/gnupg"
mkdir -m 700 "$GNUPGHOME"
export GNUPGHOME

trap 'rm -rf "${TMP_DIR}"' EXIT

cat > "$TMP_DIR/gpg-batch" << 'EOF'
%no-protection
Key-Type: RSA
Key-Length: 4096
Key-Usage: sign
Subkey-Type: RSA
Subkey-Length: 4096
Subkey-Usage: sign
Name-Real: halpradio Archive Automatic Signing Key
Name-Email: halpworld@users.noreply.github.com
Expire-Date: 0
%commit
EOF

gpg --batch --generate-key "$TMP_DIR/gpg-batch"
KEY_ID=$(gpg --list-secret-keys --with-colons | grep '^sec' | cut -d: -f5 | head -n 1)

echo "🔑 Generated Key ID: ${KEY_ID}"

# Export ASCII armored public key
gpg --armor --export "${KEY_ID}" > keys/halpradio.pub.asc

# Export dearmored (binary) keyring for apt
gpg --export "${KEY_ID}" > keys/halpradio-archive-keyring.gpg

echo "✅ Saved public keys to keys/halpradio.pub.asc and keys/halpradio-archive-keyring.gpg"

# Export private key and store in GitHub Secrets
gpg --armor --export-secret-keys "${KEY_ID}" > "$TMP_DIR/private.key"

echo "🚀 Uploading GPG_PRIVATE_KEY secret to GitHub repository..."
gh secret set GPG_PRIVATE_KEY < "$TMP_DIR/private.key"

echo "🎉 Successfully configured GPG signing key and saved to GitHub Secrets!"
