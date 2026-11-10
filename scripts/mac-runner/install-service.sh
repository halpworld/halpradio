#!/usr/bin/env bash
# Installs ephemeral-runner.sh as a launchd daemon, so the runner starts at
# boot, keeps running after you log out or close SSH, and restarts if it
# stops. It runs as you (not root); sudo is only used to place the daemon.
#
#   scripts/mac-runner/install-service.sh            install or update
#   scripts/mac-runner/install-service.sh --uninstall
#
# Install needs RUNNER_VERSION and RUNNER_SHA256 in the environment (see
# docs/SELF_HOSTED_MAC_RUNNER.md) and asks for the runner token once.
set -euo pipefail

LABEL="com.halpworld.halpradio-runner"
PLIST="/Library/LaunchDaemons/$LABEL.plist"
CONFIG_DIR="$HOME/.config/halpradio-runner"
INSTALL_DIR="$HOME/.local/share/halpradio-runner"
LOG="$HOME/Library/Logs/halpradio-runner.log"

die() { echo "error: $*" >&2; exit 1; }

[[ "$(id -u)" != 0 ]] || die "run this as your own user, not with sudo"

if [[ "${1:-}" == --uninstall ]]; then
	sudo launchctl bootout "system/$LABEL" 2>/dev/null || true
	sudo rm -f "$PLIST"
	echo "Service removed. Left in place: $CONFIG_DIR (token), $INSTALL_DIR, $LOG."
	echo "Delete them with: rm -rf '$CONFIG_DIR' '$INSTALL_DIR'"
	exit 0
fi
[[ $# == 0 ]] || die "usage: $0 [--uninstall]"

[[ "$(sysctl -n hw.optional.arm64 2>/dev/null)" == 1 && "$(sysctl -n sysctl.proc_translated 2>/dev/null)" != 1 ]] ||
	die "run this from a native Apple Silicon shell (not Rosetta)"
[[ "${RUNNER_VERSION:-}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "set RUNNER_VERSION, e.g. 2.338.2"
[[ "${RUNNER_SHA256:-}" =~ ^[0-9a-f]{64}$ ]] || die "set RUNNER_SHA256 (see docs/SELF_HOSTED_MAC_RUNNER.md)"

TOOL_PATH=""
for cmd in tart gh curl shasum caffeinate; do
	p="$(command -v "$cmd")" || die "$cmd is not installed"
	d="$(dirname "$p")"
	[[ ":$TOOL_PATH:" == *":$d:"* ]] || TOOL_PATH="${TOOL_PATH:+$TOOL_PATH:}$d"
done
for d in /usr/bin /bin /usr/sbin /sbin; do
	[[ ":$TOOL_PATH:" == *":$d:"* ]] || TOOL_PATH="$TOOL_PATH:$d"
done

umask 077
mkdir -p "$CONFIG_DIR" "$INSTALL_DIR" "$(dirname "$LOG")"
chmod 700 "$CONFIG_DIR" "$INSTALL_DIR"

TOKEN_FILE="$CONFIG_DIR/token"
if [[ ! -s "$TOKEN_FILE" ]]; then
	read -rsp "Runner token (fine-grained, halpworld/halpradio, Administration: Read and write): " token
	echo
	[[ -n "$token" ]] || die "no token entered"
	printf '%s\n' "$token" >"$TOKEN_FILE"
	unset token
fi
chmod 600 "$TOKEN_FILE"

cat >"$CONFIG_DIR/runner.env" <<EOF
# Read by ephemeral-runner.sh. KEY=value only; this file is not executed.
RUNNER_VERSION=$RUNNER_VERSION
RUNNER_SHA256=$RUNNER_SHA256
GH_TOKEN_FILE=$TOKEN_FILE
EOF
[[ -z "${TART_RUN_ARGS:-}" ]] || echo "TART_RUN_ARGS=$TART_RUN_ARGS" >>"$CONFIG_DIR/runner.env"
chmod 600 "$CONFIG_DIR/runner.env"

# The daemon runs a private copy, so a later `git pull` cannot change the
# code it runs until you install again.
SRC="$(cd "$(dirname "$0")" && pwd)/ephemeral-runner.sh"
install -m 700 "$SRC" "$INSTALL_DIR/ephemeral-runner.sh"

xml() { sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g' <<<"$1"; }
TMP_PLIST="$(mktemp)"
trap 'rm -f "$TMP_PLIST"' EXIT
cat >"$TMP_PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>$LABEL</string>
	<key>UserName</key><string>$(xml "$(id -un)")</string>
	<key>ProgramArguments</key>
	<array>
		<string>/usr/bin/caffeinate</string>
		<string>-i</string>
		<string>$(xml "$INSTALL_DIR/ephemeral-runner.sh")</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>HOME</key><string>$(xml "$HOME")</string>
		<key>PATH</key><string>$(xml "$TOOL_PATH")</string>
	</dict>
	<key>WorkingDirectory</key><string>$(xml "$HOME")</string>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>ThrottleInterval</key><integer>30</integer>
	<key>ExitTimeOut</key><integer>60</integer>
	<key>StandardOutPath</key><string>$(xml "$LOG")</string>
	<key>StandardErrorPath</key><string>$(xml "$LOG")</string>
</dict>
</plist>
EOF
plutil -lint "$TMP_PLIST" >/dev/null || die "generated plist is invalid"

echo "Installing $PLIST (sudo asks for your password)"
sudo launchctl bootout "system/$LABEL" 2>/dev/null || true
sudo install -m 644 -o root -g wheel "$TMP_PLIST" "$PLIST"
sudo launchctl bootstrap system "$PLIST"

echo "Installed. Follow the log with:  tail -f '$LOG'"
echo "Status:  sudo launchctl print system/$LABEL | grep -E 'state|pid'"
