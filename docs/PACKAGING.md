# 📦 Distribution & Packaging Guide for halpradio

This document outlines the packaging specifications, community distribution channels, and registry instructions for **halpradio**.

---

## 🍺 Homebrew (macOS & Linux)

### Official Tap Formula
`halpradio` is distributed via the official tap [`halpworld/homebrew-tap`](https://github.com/halpworld/homebrew-tap):

```bash
brew install halpworld/tap/halpradio
```

Automated release bottling is managed by `.goreleaser.yaml` on tag publication.

---

## 🍥 Debian / Ubuntu / Linux Mint / Pop!_OS (APT)

`halpradio` provides native `.deb` packages and an automated APT repository hosted via GitHub Pages, signed with GPG.

### 1. Official APT Repository (Recommended)

To install `halpradio` and receive automatic updates via `apt upgrade`:

```bash
# 1. Download the official GPG archive signing key
sudo mkdir -p /etc/apt/keyrings
curl -fsSL https://halpworld.github.io/halpradio/halpradio-archive-keyring.gpg | sudo tee /etc/apt/keyrings/halpradio-archive-keyring.gpg > /dev/null

# 2. Add the halpradio repository to your APT sources
echo "deb [signed-by=/etc/apt/keyrings/halpradio-archive-keyring.gpg] https://halpworld.github.io/halpradio stable main" | sudo tee /etc/apt/sources.list.d/halpradio.list

# 3. Update and install
sudo apt update
sudo apt install halpradio
```

### 2. Standalone `.deb` Package Download

Every release produces standalone `.deb` packages for `amd64` and `arm64`:

```bash
# Download latest .deb (amd64)
curl -LO https://github.com/halpworld/halpradio/releases/latest/download/halpradio_0.7.0_linux_amd64.deb

# Install using apt (resolves and installs recommended dependencies like mpv)
sudo apt install ./halpradio_0.7.0_linux_amd64.deb
```

---

## 🐧 Arch Linux (AUR)

### PKGBUILD Template (`halpradio-bin`)

For Arch Linux users, the binary package can be built using the following `PKGBUILD`:

```bash
# Maintainer: halpworld <https://github.com/halpworld>
pkgname=halpradio-bin
pkgver=0.7.0
pkgrel=1
pkgdesc="LazyVim-inspired Terminal Internet Radio Streamer"
arch=('x86_64' 'aarch64')
url="https://github.com/halpworld/halpradio"
license=('GPL-3.0-or-later')
optdepends=(
    'mpv: Recommended audio backend for AAC/OGG/HLS support'
    'vlc: Alternative audio backend'
)
provides=('halpradio')
conflicts=('halpradio')

source_x86_64=("https://github.com/halpworld/halpradio/releases/download/v${pkgver}/halpradio_${pkgver}_linux_amd64.tar.gz")
source_aarch64=("https://github.com/halpworld/halpradio/releases/download/v${pkgver}/halpradio_${pkgver}_linux_arm64.tar.gz")
sha256sums_x86_64=('SKIP')
sha256sums_aarch64=('SKIP')

package() {
    install -Dm755 "${srcdir}/halpradio" "${pkgdir}/usr/bin/halpradio"
    install -Dm644 "${srcdir}/README.md" "${pkgdir}/usr/share/doc/${pkgname}/README.md"
    install -Dm644 "${srcdir}/LICENSE" "${pkgdir}/usr/share/licenses/${pkgname}/LICENSE"
}
```

---

## 🐳 Docker Container

You can run `halpradio` in a container with audio mapped to host ALSA / PulseAudio / PipeWire:

```dockerfile
# Multi-stage lightweight build
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o halpradio main.go

FROM alpine:3.19
RUN apk add --no-cache mpv ca-certificates tzdata alsa-utils
COPY --from=builder /app/halpradio /usr/local/bin/halpradio
ENTRYPOINT ["halpradio"]
```

### Quick Run Command:
```bash
docker run --rm -it \
  --device /dev/snd \
  -e TERM=$TERM \
  halpradio:latest
```

---

## 🪟 Windows (Scoop Manifest)

For Windows users using [Scoop](https://scoop.sh):

```json
{
    "version": "0.7.0",
    "description": "LazyVim-inspired Terminal Internet Radio Streamer",
    "homepage": "https://github.com/halpworld/halpradio",
    "license": "GPL-3.0-or-later",
    "architecture": {
        "64bit": {
            "url": "https://github.com/halpworld/halpradio/releases/download/v0.7.0/halpradio_0.7.0_windows_amd64.zip",
            "bin": "halpradio.exe"
        },
        "arm64": {
            "url": "https://github.com/halpworld/halpradio/releases/download/v0.7.0/halpradio_0.7.0_windows_arm64.zip",
            "bin": "halpradio.exe"
        }
    }
}
```

---

## ❄️ Nix / Nixpkgs

```nix
{ lib, buildGoModule, fetchFromGitHub }:

buildGoModule rec {
  pname = "halpradio";
  version = "0.7.0";

  src = fetchFromGitHub {
    owner = "halpworld";
    repo = "halpradio";
    rev = "v${version}";
    hash = "sha256-...";
  };

  vendorHash = null;

  meta = with lib; {
    description = "LazyVim-inspired Terminal Internet Radio Streamer";
    homepage = "https://github.com/halpworld/halpradio";
    license = licenses.gpl3Plus;
    maintainers = [ ];
    mainProgram = "halpradio";
  };
}
```
