# Self-hosted macOS CI runner

GitHub-hosted macOS runners are sometimes unavailable ("The job was not
acquired by Runner of type hosted"). A maintainer can run the
`Test (macos-latest)` CI job on their own Apple Silicon Mac instead. This
page explains how, and how the setup keeps that Mac safe.

## Security model

`halpradio` is a public repository. A CI job runs whatever code the branch
contains, so a runner is only as safe as the code it accepts. The setup
has four layers:

1. **Only trusted code is routed to it.** In `.github/workflows/ci.yml`, the
   macOS job uses the self-hosted runner only for pushes to `main`, the
   weekly scheduled run, and pull requests from branches in this repository
   that Dependabot did not open. Pull requests from forks and from
   Dependabot always use GitHub-hosted runners.
2. **Every job runs in a new VM.** `scripts/mac-runner/ephemeral-runner.sh`
   clones a fresh [Tart](https://tart.run) macOS VM for each job and deletes
   it afterwards. Jobs never run on the host, and nothing carries over
   from one job to the next.
3. **The VM holds no lasting credential.** Each VM gets a single-use JIT
   runner config, which GitHub invalidates after one job. The token that
   creates these configs stays on the host. The runner tarball is shared
   into the VM read-only. The VM cannot see your home folder.
4. **The runner is checked before use.** The runner download must match the
   SHA-256 you set in `RUNNER_SHA256`, or the script deletes it and stops.

**What this does not cover:**

- **Network access.** By default the VM can reach your home network. See
  [Network isolation](#network-isolation).
- **The job token.** A job running on the VM still receives that job's
  `GITHUB_TOKEN` (`contents: read`, `pull-requests: write`).

Do not run the runner directly on a machine that holds other credentials,
such as an agent server.

## One-time setup

On the Mac (Apple Silicon, macOS 14 or later, about 60 GB free):

```bash
brew install cirruslabs/cli/tart gh
```

Create a fine-grained personal access token at
<https://github.com/settings/personal-access-tokens/new>:

- **Resource owner:** `halpworld`
- **Repository access:** only `halpworld/halpradio`
- **Permissions:** Repository → Administration → Read and write (the only
  one needed; it lets the script create JIT runner configs)
- **Expiry:** short, for example 90 days

In the repository settings on GitHub:

1. **Actions → General → Approval for running fork pull request workflows:**
   choose "Require approval for all external contributors". This is defence in
   depth on top of the routing rule.
2. **Secrets and variables → Actions → Variables:** add `MACOS_SELF_HOSTED`
   with the value `true`.

## Running it

Pick a runner release from <https://github.com/actions/runner/releases> and
read its `osx-arm64` digest from the release notes:

```bash
V=2.338.2
gh api "repos/actions/runner/releases/tags/v$V" --jq .body |
  sed -n 's/.*BEGIN SHA osx-arm64 -->\([0-9a-f]*\)<.*/\1/p'
```

Then start the loop. It serves one job per VM until you stop it with Ctrl-C:

```bash
RUNNER_VERSION=2.338.2 RUNNER_SHA256=<digest> GH_TOKEN=<token> \
  scripts/mac-runner/ephemeral-runner.sh
```

Set `ONCE=1` to serve a single job and exit. `VM_CPU` and `VM_MEMORY_MB`
size the VM (defaults 4 and 8192). The race-detector test run needs at
least 8 GB.

The first run pulls `ghcr.io/cirruslabs/macos-sequoia-base:latest`, about
25 GB. After that, cloning a VM takes seconds.

## Network isolation

Tart's `--net-softnet` mode isolates the VM from the host and from other
VMs. It needs a one-time privileged setup; see `tart run --help`. Pass it
with `TART_RUN_ARGS="--net-softnet"`. To also keep the VM off your LAN,
check `tart run --help` for the softnet block/allow options in your Tart
version and block your LAN range, for example `192.168.0.0/16`.

## Turning it off

Delete or change the `MACOS_SELF_HOSTED` variable. The macOS job goes back
to `macos-latest` on the next run.

If the variable is `true` but no runner is online, macOS jobs wait in the
queue, for up to 24 hours. Turn the variable off whenever the Mac will be
away.

Revoke the token when you stop using the runner.
