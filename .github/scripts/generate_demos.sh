#!/usr/bin/env bash
#
# Generates the animated demo and static screenshots used in the README by
# replaying the VHS tape files in vhs/. It builds the current source into a
# local binary, seeds a deterministic configuration (no network / desktop
# integration), and renders each tape into docs/images/.
#
# Requires `vhs` (and its dependencies ttyd + ffmpeg) on the PATH.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

# Build the binary the tapes will launch.
echo "==> Building halpradio"
CGO_ENABLED=0 go build -o halpradio main.go
trap 'rm -f "$ROOT_DIR/halpradio"' EXIT

# Isolate the config directory so the demo is deterministic and offline:
# disable the catalog auto-update, desktop notifications, MPRIS, IPC, and
# Discord RPC. XDG_CONFIG_HOME covers Linux CI; the second path covers macOS.
CONFIG_ROOT="$(mktemp -d)"
export HOME="$CONFIG_ROOT/home"
export XDG_CONFIG_HOME="$CONFIG_ROOT/config"
mkdir -p "$XDG_CONFIG_HOME/halpradio"
mkdir -p "$HOME/Library/Application Support/halpradio"

cat > "$XDG_CONFIG_HOME/halpradio/config.yaml" <<'EOF'
volume: 80
player_backend: native
theme: tokyonight
visualizer_mode: dj-cat
song_notifications: false
mpris_enabled: false
ipc_enabled: false
discord_rpc: false
catalog_auto_update: false
EOF
cp "$XDG_CONFIG_HOME/halpradio/config.yaml" "$HOME/Library/Application Support/halpradio/config.yaml"

# Render every tape in vhs/ (screenshots + animated demo).
for tape in vhs/*.tape; do
  echo "==> Rendering $tape"
  vhs "$tape"
done

echo "==> Done"
