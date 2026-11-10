#!/usr/bin/env bash
# ==============================================================================
# build_apt_repo.sh — Automated Debian/Ubuntu APT Repository Generator
# ==============================================================================
# Generates Debian repository layout:
#   pool/main/*.deb
#   dists/stable/main/binary-amd64/{Packages, Packages.gz}
#   dists/stable/main/binary-arm64/{Packages, Packages.gz}
#   dists/stable/{Release, Release.gpg, InRelease}
#   KEY.gpg & halpradio-archive-keyring.gpg
#   index.html (Landing page with install instructions)
#
# Usage:
#   CHECKSUMS_FILE=dist/checksums.txt SIGNING_KEY_FPR=<40-hex fingerprint> \
#     scripts/build_apt_repo.sh <deb-src-dir> <repo-dir>
#
# Security properties:
#   - The pool is rebuilt from scratch: only the .deb files of the current
#     release are indexed and signed. Anything already in <repo-dir> (e.g. a
#     package pushed to gh-pages by someone without the signing key) is removed
#     instead of being signed.
#   - Every .deb must be listed in the release's checksums.txt with a matching
#     SHA-256, or the script fails.
#   - Signing requires the key whose fingerprint is SIGNING_KEY_FPR to be in
#     the keyring; there is no unsigned fallback. The signature is checked with
#     gpgv against the committed keys/halpradio-archive-keyring.gpg, and only
#     that committed keyring is published (never a key exported from the CI
#     keyring).
#   - Release carries Valid-Until (APT_VALID_DAYS, default 14 days after
#     Date), so apt refuses a stale or replayed index once it expires. The
#     repository must therefore be re-signed at least that often, not only
#     when a release is made.
# ==============================================================================

set -euo pipefail

DEB_SRC_DIR="${1:-dist}"
REPO_DIR="${2:-apt-repo}"
CHECKSUMS_FILE="${CHECKSUMS_FILE:-${DEB_SRC_DIR}/checksums.txt}"
SIGNING_KEY_FPR="${SIGNING_KEY_FPR:-}"
APT_VALID_DAYS="${APT_VALID_DAYS:-14}"
PUBLIC_KEYRING="keys/halpradio-archive-keyring.gpg"
PUBLIC_KEY_ASC="keys/halpradio.pub.asc"

die() {
  echo "❌ $*" >&2
  exit 1
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

[[ "${SIGNING_KEY_FPR}" =~ ^[0-9A-F]{40}$ ]] ||
  die "SIGNING_KEY_FPR must be the 40-hex-digit (upper case) fingerprint of the APT signing key"
[[ "${APT_VALID_DAYS}" =~ ^[1-9][0-9]?$ ]] ||
  die "APT_VALID_DAYS must be a whole number of days from 1 to 99"
[ -f "${CHECKSUMS_FILE}" ] || die "checksums file not found: ${CHECKSUMS_FILE}"
[ -f "${PUBLIC_KEYRING}" ] || die "public keyring not found: ${PUBLIC_KEYRING} (run from the repository root)"
gpg --batch --list-secret-keys "${SIGNING_KEY_FPR}" >/dev/null 2>&1 ||
  die "secret key ${SIGNING_KEY_FPR} is not in the keyring; refusing to build an unsigned repository"

shopt -s nullglob
new_debs=("${DEB_SRC_DIR}"/*.deb)
shopt -u nullglob
[ "${#new_debs[@]}" -gt 0 ] || die "no .deb packages found in ${DEB_SRC_DIR}"

# Verify every package against the release checksums before it goes anywhere.
for deb in "${new_debs[@]}"; do
  name="$(basename "${deb}")"
  [[ "${name}" =~ ^[A-Za-z0-9._+~-]+\.deb$ ]] || die "unexpected package file name: ${name}"
  expected="$(awk -v f="${name}" '$2 == f || $2 == "*" f { print $1 }' "${CHECKSUMS_FILE}")"
  [[ "${expected}" =~ ^[0-9a-f]{64}$ ]] || die "${name} is not listed (exactly once) in ${CHECKSUMS_FILE}"
  actual="$(sha256_of "${deb}")"
  [ "${actual}" = "${expected}" ] || die "SHA-256 mismatch for ${name}: expected ${expected}, got ${actual}"
  echo "✅ ${name} matches ${CHECKSUMS_FILE}"
done

echo "📦 Rebuilding APT repository in: ${REPO_DIR}"
mkdir -p "${REPO_DIR}"
# Drop everything previously served from the pool and indexes; only this
# release's verified packages are indexed and signed below.
rm -rf "${REPO_DIR}/pool" "${REPO_DIR}/dists"
mkdir -p "${REPO_DIR}/pool/main"
mkdir -p "${REPO_DIR}/dists/stable/main/binary-amd64"
mkdir -p "${REPO_DIR}/dists/stable/main/binary-arm64"

echo "📥 Copying verified deb packages to ${REPO_DIR}/pool/main/..."
cp -v "${new_debs[@]}" "${REPO_DIR}/pool/main/"

# Run metadata indexing via Python (zero external dependencies required)
python3 - "${REPO_DIR}" "${APT_VALID_DAYS}" << 'EOF'
import os
import sys
import io
import gzip
import tarfile
import hashlib
from datetime import datetime, timedelta, timezone

repo_dir = sys.argv[1]
pool_dir = os.path.join(repo_dir, "pool", "main")
dists_dir = os.path.join(repo_dir, "dists", "stable")

if not os.path.exists(pool_dir):
    print("Pool directory does not exist, skipping indexing.")
    sys.exit(0)

deb_files = [f for f in os.listdir(pool_dir) if f.endswith(".deb")]
print(f"🔍 Found {len(deb_files)} package(s) in {pool_dir}")

arch_packages = {"amd64": [], "arm64": []}

def parse_ar_deb(file_path):
    with open(file_path, "rb") as f:
        content = f.read()

    if not content.startswith(b"!<arch>\n"):
        raise ValueError(f"{file_path} is not a valid ar archive")

    idx = 8
    control_content = None
    while idx < len(content):
        header = content[idx:idx+60]
        if len(header) < 60:
            break
        filename = header[:16].decode("ascii", errors="replace").strip()
        size_str = header[48:58].decode("ascii", errors="replace").strip()
        size = int(size_str)
        idx += 60
        data = content[idx:idx+size]
        idx += size
        if idx % 2 != 0:
            idx += 1  # ar 2-byte boundary alignment

        if filename.startswith("control.tar"):
            with tarfile.open(fileobj=io.BytesIO(data), mode="r:*") as tar:
                for member in tar.getmembers():
                    if member.name.endswith("./control") or member.name == "control":
                        f_extracted = tar.extractfile(member)
                        if f_extracted:
                            control_content = f_extracted.read().decode("utf-8", errors="replace")
                            break
            break

    if not control_content:
        raise ValueError(f"Could not find control file in {file_path}")

    # Calculate hashes
    md5 = hashlib.md5(content).hexdigest()
    sha1 = hashlib.sha1(content).hexdigest()
    sha256 = hashlib.sha256(content).hexdigest()
    file_size = len(content)

    return control_content.strip(), md5, sha1, sha256, file_size

for deb in sorted(deb_files):
    deb_path = os.path.join(pool_dir, deb)
    rel_path = f"pool/main/{deb}"
    try:
        ctl, md5, sha1, sha256, file_size = parse_ar_deb(deb_path)
    except Exception as e:
        print(f"❌ Error parsing {deb}: {e}")
        sys.exit(1)

    # Extract Architecture
    arch = None
    for line in ctl.splitlines():
        if line.lower().startswith("architecture:"):
            arch = line.split(":", 1)[1].strip()
            break

    entry = f"{ctl}\nFilename: {rel_path}\nSize: {file_size}\nMD5sum: {md5}\nSHA1: {sha1}\nSHA256: {sha256}\n\n"

    if arch in arch_packages:
        arch_packages[arch].append(entry)
    elif arch == "all":
        arch_packages["amd64"].append(entry)
        arch_packages["arm64"].append(entry)
    else:
        # Unknown arch, fallback to amd64
        arch_packages["amd64"].append(entry)

# Write Packages and Packages.gz
for arch, packages in arch_packages.items():
    arch_dir = os.path.join(dists_dir, "main", f"binary-{arch}")
    os.makedirs(arch_dir, exist_ok=True)
    pkg_file = os.path.join(arch_dir, "Packages")
    pkg_gz = os.path.join(arch_dir, "Packages.gz")

    content = "".join(packages).encode("utf-8")
    with open(pkg_file, "wb") as f:
        f.write(content)

    with gzip.open(pkg_gz, "wb", compresslevel=9) as f:
        f.write(content)

    print(f"✅ Generated {pkg_file} ({len(packages)} entries) and {pkg_gz}")

# Generate Release file
release_files = [
    "main/binary-amd64/Packages",
    "main/binary-amd64/Packages.gz",
    "main/binary-arm64/Packages",
    "main/binary-arm64/Packages.gz",
]

def file_hashes(rel_path):
    full_path = os.path.join(dists_dir, rel_path)
    with open(full_path, "rb") as f:
        data = f.read()
    return (
        len(data),
        hashlib.md5(data).hexdigest(),
        hashlib.sha1(data).hexdigest(),
        hashlib.sha256(data).hexdigest()
    )

valid_days = int(sys.argv[2])
now = datetime.now(timezone.utc)
date_str = now.strftime("%a, %d %b %Y %H:%M:%S UTC")
valid_until_str = (now + timedelta(days=valid_days)).strftime("%a, %d %b %Y %H:%M:%S UTC")

release_content = [
    "Origin: halpradio",
    "Label: halpradio",
    "Suite: stable",
    "Codename: stable",
    "Version: 1.0",
    "Architectures: amd64 arm64",
    "Components: main",
    "Description: halpradio Official APT Repository",
    f"Date: {date_str}",
    f"Valid-Until: {valid_until_str}",
    "MD5Sum:"
]

for rf in release_files:
    size, md5, _, _ = file_hashes(rf)
    release_content.append(f" {md5} {size:>8} {rf}")

release_content.append("SHA1:")
for rf in release_files:
    size, _, sha1, _ = file_hashes(rf)
    release_content.append(f" {sha1} {size:>8} {rf}")

release_content.append("SHA256:")
for rf in release_files:
    size, _, _, sha256 = file_hashes(rf)
    release_content.append(f" {sha256} {size:>8} {rf}")

release_path = os.path.join(dists_dir, "Release")
with open(release_path, "w", encoding="utf-8") as f:
    f.write("\n".join(release_content) + "\n")

print(f"✅ Generated {release_path}")
EOF

# Publish only the committed public key material.
cp "${PUBLIC_KEYRING}" "${REPO_DIR}/halpradio-archive-keyring.gpg"
if [ -f "${PUBLIC_KEY_ASC}" ]; then
  cp "${PUBLIC_KEY_ASC}" "${REPO_DIR}/KEY.gpg"
fi

# GPG Signing with exactly the expected key (gpg picks its signing subkey);
# any other key that happens to be in the keyring is never used.
echo "🔏 Signing Release file with key ${SIGNING_KEY_FPR}"
gpg --batch --yes --armor --detach-sign --local-user "${SIGNING_KEY_FPR}" \
  -o "${REPO_DIR}/dists/stable/Release.gpg" "${REPO_DIR}/dists/stable/Release"
gpg --batch --yes --clearsign --local-user "${SIGNING_KEY_FPR}" \
  -o "${REPO_DIR}/dists/stable/InRelease" "${REPO_DIR}/dists/stable/Release"

# Prove that what we publish verifies against the keyring users install.
gpgv --keyring "${PWD}/${PUBLIC_KEYRING}" \
  "${REPO_DIR}/dists/stable/Release.gpg" "${REPO_DIR}/dists/stable/Release" ||
  die "Release.gpg does not verify against ${PUBLIC_KEYRING}"
gpgv --keyring "${PWD}/${PUBLIC_KEYRING}" "${REPO_DIR}/dists/stable/InRelease" ||
  die "InRelease does not verify against ${PUBLIC_KEYRING}"
echo "✅ InRelease and Release.gpg created and verified against ${PUBLIC_KEYRING}."

# Generate stylish index.html landing page for GitHub Pages
cat > "${REPO_DIR}/index.html" << 'EOF'
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>halpradio APT Repository</title>
  <style>
    :root {
      --bg: #1a1b26;
      --card-bg: #24283b;
      --text: #c0caf5;
      --heading: #7aa2f7;
      --accent: #bb9af7;
      --green: #9ece6a;
      --border: #414868;
      --code-bg: #16161e;
    }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
      background-color: var(--bg);
      color: var(--text);
      line-height: 1.6;
      margin: 0;
      padding: 2rem 1rem;
      display: flex;
      justify-content: center;
    }
    .container {
      max-width: 800px;
      width: 100%;
    }
    header {
      text-align: center;
      margin-bottom: 2rem;
    }
    h1 {
      color: var(--heading);
      margin-bottom: 0.2rem;
      font-size: 2.2rem;
    }
    .tagline {
      color: var(--accent);
      font-size: 1.1rem;
    }
    .card {
      background: var(--card-bg);
      border: 1px solid var(--border);
      border-radius: 8px;
      padding: 1.5rem;
      margin-bottom: 1.5rem;
      box-shadow: 0 4px 6px rgba(0,0,0,0.3);
    }
    h2 {
      color: var(--green);
      margin-top: 0;
      font-size: 1.4rem;
    }
    pre {
      background: var(--code-bg);
      border: 1px solid var(--border);
      border-radius: 6px;
      padding: 1rem;
      overflow-x: auto;
      font-family: "JetBrains Mono", Consolas, Menlo, Monaco, monospace;
      font-size: 0.95rem;
      color: #7dcfff;
    }
    a {
      color: var(--heading);
      text-decoration: none;
    }
    a:hover {
      text-decoration: underline;
    }
    footer {
      text-align: center;
      margin-top: 3rem;
      color: #565f89;
      font-size: 0.9rem;
    }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <h1>📻 halpradio Official APT Repository</h1>
      <p class="tagline">LazyVim-inspired Terminal Internet Radio Streamer</p>
    </header>

    <div class="card">
      <h2>🚀 Quick Install on Debian, Ubuntu, Linux Mint & Pop!_OS</h2>
      <p>Run the following commands in your terminal to add the repository and install <code>halpradio</code>:</p>
      <pre># 1. Download official GPG archive signing key
sudo mkdir -p /etc/apt/keyrings
curl -fsSL https://halpworld.github.io/halpradio/halpradio-archive-keyring.gpg | sudo tee /etc/apt/keyrings/halpradio-archive-keyring.gpg &gt; /dev/null

# 2. Add halpradio repository to APT sources
echo "deb [signed-by=/etc/apt/keyrings/halpradio-archive-keyring.gpg] https://halpworld.github.io/halpradio stable main" | sudo tee /etc/apt/sources.list.d/halpradio.list

# 3. Update index and install
sudo apt update
sudo apt install halpradio</pre>
    </div>

    <div class="card">
      <h2>📦 Direct .deb Download</h2>
      <p>You can also download individual packages directly:</p>
      <ul>
        <li>Browse repository packages: <a href="pool/main/"><code>pool/main/</code></a></li>
        <li>GitHub Releases: <a href="https://github.com/halpworld/halpradio/releases">halpworld/halpradio/releases</a></li>
      </ul>
      <p>Install downloaded package directly with automatic dependency resolution:</p>
      <pre>sudo apt install ./halpradio_*.deb</pre>
    </div>

    <footer>
      <p>Project homepage: <a href="https://github.com/halpworld/halpradio">github.com/halpworld/halpradio</a></p>
    </footer>
  </div>
</body>
</html>
EOF

echo "🎉 APT repository generated successfully at ${REPO_DIR}!"
