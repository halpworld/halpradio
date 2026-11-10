# Security Policy

## Supported Versions

halpradio has no maintenance branches. Security fixes are released only as a new
version on top of the latest release, and every install channel (Homebrew tap,
installer script, release downloads) only moves forward.

| Version                    | Supported          |
| -------------------------- | ------------------ |
| Latest release (currently 0.8.x) | :white_check_mark: |
| Anything older             | :x:                |

If you run an older version, upgrade to the latest release.

## Reporting a Vulnerability

Please **do not open a public issue** for a security problem.

Report it privately through
[GitHub Private Vulnerability Reporting](https://github.com/halpworld/halpradio/security/advisories/new)
(the repository's **Security** tab, then **Report a vulnerability**). This is the
only reporting channel.

Include the version (`halpradio -version`), your OS, steps to reproduce, and a
proof of concept if you have one. halpradio is maintained by volunteers: we aim to
acknowledge reports within a few days and will keep you updated in the advisory
until a fix is released.

## Verifying a download

Every release publishes `checksums.txt` with the SHA-256 of each archive and
`.deb` package. Check a download against it:

```bash
sha256sum --ignore-missing -c checksums.txt          # Linux
shasum -a 256 --ignore-missing -c checksums.txt      # macOS
```

A checksum from the same release only detects a damaged download. To check that a
file was built by this project's release workflow, use one of the following. They
are available for releases made **after v0.8.1**; v0.8.1 and earlier have
checksums only.

**Sigstore signature on `checksums.txt`** (needs [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) v2.4 or later).
Replace `vX.Y.Z` with the release tag; then check the files with `checksums.txt` as above:

```bash
cosign verify-blob \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity "https://github.com/halpworld/halpradio/.github/workflows/release.yml@refs/tags/vX.Y.Z" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  checksums.txt
```

**GitHub build provenance** for any single release file (needs the GitHub CLI, signed in):

```bash
gh attestation verify halpradio_X.Y.Z_linux_amd64.tar.gz --repo halpworld/halpradio
```

Each release also includes an SPDX SBOM (`*.sbom.json`) for every archive.

The [installer script](./install.sh) always verifies the SHA-256 and stops on a
mismatch, and prints which checks it performed. When `cosign` is installed it also
verifies the signature:

- A signature that does not verify always stops the install.
- For a release after v0.8.1, a missing or unfetchable `checksums.txt.sigstore.json`
  stops the install, unless `HALPRADIO_ALLOW_UNSIGNED=1` is set (it then continues
  with a warning).
- v0.8.1 and older releases have no signature; the installer says so and continues
  on the checksum only.
- Without `cosign` it continues on the checksum only and says the signature was NOT
  checked.

Environment variables: `VERSION=vX.Y.Z` picks a release, `INSTALL_DIR` the target
directory. `HALPRADIO_REQUIRE_SIGNATURE=1` stops unless the signature was verified,
for every version, including when `cosign` is missing; it wins over
`HALPRADIO_ALLOW_UNSIGNED=1`. See [docs/PACKAGING.md](./docs/PACKAGING.md#-install-script-and-signatures).

The Homebrew formula in `halpworld/homebrew-tap` pins the SHA-256 of each
archive.

**APT repository** (not served yet, see the README): `Release`/`InRelease` are
signed with the archive key whose primary key fingerprint is

```
B2B5 AB26 19FF 9D6A 7EA6  30CA 79E3 1FF2 2916 CAAE
```

Compare it with `gpg --show-keys /etc/apt/keyrings/halpradio-archive-keyring.gpg`.
If the key is ever rotated, the new fingerprint will be announced here and in the
release notes.

This committed key (under `keys/`) is the old archive key and never expires. The
owner must rotate it with `scripts/setup_apt_gpg.sh` before the APT repository is
announced, and replace the fingerprint above. The signed `Release` file carries
`Valid-Until` (14 days), so apt rejects an expired or replayed index.

**Container images:** none are published. Build from the repository's
`Dockerfile`; do not trust a `halpradio` image from any public registry.

macOS and Windows binaries are not code-signed or notarized by Apple or Microsoft.

## Security Best Practices for Users

- **Streaming URLs**: `halpradio` plays user-provided and community-submitted audio URLs. We recommend streaming over HTTPS whenever possible.
- **Custom Config**: Custom stations are stored locally in `~/.config/halpradio/stations.yaml`. Keep this directory with standard user read/write permissions.
