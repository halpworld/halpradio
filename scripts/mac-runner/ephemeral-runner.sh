#!/usr/bin/env bash
# Runs halpradio's macOS CI jobs on this Mac, one job per throwaway Tart VM.
#
# Each loop iteration clones a fresh VM from a base image, registers a
# single-use (JIT) GitHub runner inside it, waits for that runner to take
# and finish one job, then deletes the VM. Nothing a job does survives to
# the next job, and jobs never run on the host itself.
#
# See docs/SELF_HOSTED_MAC_RUNNER.md for setup and the security model.
#
# Required environment:
#   RUNNER_VERSION   actions/runner release, e.g. 2.330.0 (no leading v)
#   RUNNER_SHA256    SHA-256 of actions-runner-osx-arm64-$RUNNER_VERSION.tar.gz
#                    as published in that release's notes
#   GH_TOKEN         fine-grained token for halpworld/halpradio only, with
#                    "Administration: Read and write" (used to mint JIT runner
#                    configs; it never enters the VM)
# Optional:
#   REPO             default halpworld/halpradio
#   BASE_IMAGE       default ghcr.io/cirruslabs/macos-sequoia-base:latest
#   VM_CPU / VM_MEMORY_MB   default 4 / 8192
#   TART_RUN_ARGS    extra `tart run` flags, e.g. --net-softnet
#   ONCE=1           run a single job and exit
#   GH_TOKEN_FILE    read GH_TOKEN from this file instead (mode 600)
#   RUNNER_CONFIG    KEY=value file read at start (default
#                    ~/.config/halpradio-runner/runner.env, used by the
#                    launchd service from install-service.sh). Only the
#                    variables above are accepted; the file is not executed.
set -euo pipefail

die() { echo "error: $*" >&2; exit 1; }
log() { echo "[$(date '+%H:%M:%S')] $*"; }

# private_file PATH: refuses a file other users could read or change.
private_file() {
	[[ -f "$1" && ! -L "$1" ]] || die "$1 is missing or not a regular file"
	[[ "$(stat -f '%u' "$1")" == "$(id -u)" ]] || die "$1 is not owned by you"
	[[ "$(stat -f '%Lp' "$1")" =~ ^[67]00$ ]] || die "$1 must be mode 600 (chmod 600 '$1')"
}

RUNNER_CONFIG="${RUNNER_CONFIG:-$HOME/.config/halpradio-runner/runner.env}"
if [[ -e "$RUNNER_CONFIG" ]]; then
	private_file "$RUNNER_CONFIG"
	while IFS= read -r line || [[ -n "$line" ]]; do
		[[ -z "$line" || "$line" == \#* ]] && continue
		[[ "$line" =~ ^(RUNNER_VERSION|RUNNER_SHA256|GH_TOKEN_FILE|REPO|BASE_IMAGE|VM_CPU|VM_MEMORY_MB|TART_RUN_ARGS)=(.*)$ ]] ||
			die "$RUNNER_CONFIG: unsupported line: ${line%%=*}"
		# Values from the environment win over the file.
		[[ -n "${!BASH_REMATCH[1]:-}" ]] || export "${BASH_REMATCH[1]}=${BASH_REMATCH[2]}"
	done <"$RUNNER_CONFIG"
fi
if [[ -z "${GH_TOKEN:-}" && -n "${GH_TOKEN_FILE:-}" ]]; then
	private_file "$GH_TOKEN_FILE"
	GH_TOKEN="$(tr -d '[:space:]' <"$GH_TOKEN_FILE")"
	export GH_TOKEN
fi

REPO="${REPO:-halpworld/halpradio}"
BASE_IMAGE="${BASE_IMAGE:-ghcr.io/cirruslabs/macos-sequoia-base:latest}"
VM_CPU="${VM_CPU:-4}"
VM_MEMORY_MB="${VM_MEMORY_MB:-8192}"
LABELS='["self-hosted","macOS","ARM64","halpradio-mac"]'
CACHE_DIR="${CACHE_DIR:-$HOME/Library/Caches/halpradio-runner}"

for cmd in tart gh curl shasum; do
	command -v "$cmd" >/dev/null || die "$cmd is not installed"
done
[[ "$(uname -s)" == Darwin && "$(sysctl -n hw.optional.arm64 2>/dev/null)" == 1 ]] ||
	die "needs an Apple Silicon Mac"
# Under Rosetta, uname -m says x86_64 and Intel-only tools are picked up.
[[ "$(sysctl -n sysctl.proc_translated 2>/dev/null)" != 1 ]] ||
	die "this shell runs under Rosetta; start a native one (arch -arm64 /bin/zsh) and see docs/SELF_HOSTED_MAC_RUNNER.md"
[[ -n "${RUNNER_VERSION:-}" ]] || die "set RUNNER_VERSION (see docs/SELF_HOSTED_MAC_RUNNER.md)"
[[ "$RUNNER_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "RUNNER_VERSION must look like 2.330.0"
[[ "${RUNNER_SHA256:-}" =~ ^[0-9a-f]{64}$ ]] || die "set RUNNER_SHA256 to the 64-hex digest from the release notes"
[[ -n "${GH_TOKEN:-}" ]] || die "set GH_TOKEN (fine-grained, this repo only, Administration: Read and write)"
tart exec --help >/dev/null 2>&1 || die "this tart has no 'exec' command; update tart (brew upgrade cirruslabs/cli/tart)"

# Download and verify the runner once; the VM gets it through a read-only share.
TARBALL="actions-runner-osx-arm64-${RUNNER_VERSION}.tar.gz"
SHARE_DIR="$CACHE_DIR/$RUNNER_VERSION"
mkdir -p "$SHARE_DIR"
chmod 700 "$CACHE_DIR"
if [[ ! -f "$SHARE_DIR/$TARBALL" ]]; then
	log "downloading $TARBALL"
	curl -fsSL --proto '=https' --tlsv1.2 -o "$SHARE_DIR/$TARBALL.part" \
		"https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/${TARBALL}"
	mv "$SHARE_DIR/$TARBALL.part" "$SHARE_DIR/$TARBALL"
fi
echo "$RUNNER_SHA256  $SHARE_DIR/$TARBALL" | shasum -a 256 -c --status ||
	{ rm -f "$SHARE_DIR/$TARBALL"; die "runner checksum mismatch; deleted the download"; }

log "pulling $BASE_IMAGE (first run downloads ~25 GB)"
tart pull "$BASE_IMAGE"

# A crash or power cut can leave job VMs behind; delete stopped ones.
# shellcheck disable=SC2013 # one VM name per line
for stale in $(tart list --source local 2>/dev/null |
	awk '$2 ~ /^halpradio-runner-[0-9]+-[0-9]+$/ && tolower($NF) == "stopped" { print $2 }'); do
	log "deleting leftover VM $stale"
	tart delete "$stale" >/dev/null 2>&1 || true
done

VM=""
RUN_PID=""
cleanup() {
	if [[ -n "$VM" ]]; then
		tart stop "$VM" >/dev/null 2>&1 || true
		[[ -n "$RUN_PID" ]] && wait "$RUN_PID" 2>/dev/null || true
		tart delete "$VM" >/dev/null 2>&1 || true
		log "deleted $VM"
	fi
	VM=""
	RUN_PID=""
}
trap 'cleanup; exit 130' INT TERM
trap cleanup EXIT

run_one_job() {
	VM="halpradio-runner-$(date +%s)-$RANDOM"
	log "cloning $VM"
	tart clone "$BASE_IMAGE" "$VM"
	tart set "$VM" --cpu "$VM_CPU" --memory "$VM_MEMORY_MB"

	# shellcheck disable=SC2086 # TART_RUN_ARGS is a list of flags
	tart run "$VM" --no-graphics --dir="runner:$SHARE_DIR:ro" ${TART_RUN_ARGS:-} &
	RUN_PID=$!

	log "waiting for the VM to boot"
	local ready=0
	for _ in $(seq 1 90); do
		if tart exec "$VM" true >/dev/null 2>&1; then ready=1; break; fi
		sleep 2
	done
	[[ $ready == 1 ]] || { log "VM did not come up"; cleanup; return 1; }

	# A JIT config registers a runner that takes exactly one job and is
	# removed by GitHub afterwards. It is the only credential the VM sees.
	local jit
	jit="$(gh api -X POST "repos/$REPO/actions/runners/generate-jitconfig" \
		--input <(printf '{"name":"%s","runner_group_id":1,"work_folder":"_work","labels":%s}' "$VM" "$LABELS") \
		--jq .encoded_jit_config)"
	[[ -n "$jit" ]] || { log "could not get a JIT runner config"; cleanup; return 1; }

	log "runner $VM is waiting for a job"
	# Runs inside the VM: $1 is the JIT config, $2 the runner tarball name.
	# shellcheck disable=SC2016
	tart exec "$VM" /bin/bash -c '
		set -euo pipefail
		mkdir -p ~/actions-runner && cd ~/actions-runner
		tar xzf "/Volumes/My Shared Files/runner/$2"
		./run.sh --jitconfig "$1"
	' _ "$jit" "$TARBALL" || log "runner exited with status $?"

	cleanup
}

while true; do
	run_one_job || sleep 30
	[[ "${ONCE:-}" == 1 ]] && break
done
