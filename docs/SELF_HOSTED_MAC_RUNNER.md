# Self-hosted macOS CI runner

GitHub-hosted macOS runners are sometimes unavailable ("The job was not
acquired by Runner of type hosted"). A maintainer can run the
`Test (macos-latest)` CI job on their own Apple Silicon Mac instead. This
page explains how, and how the setup keeps that Mac safe.

## Security model

`halpradio` is a public repository. A CI job runs whatever code the branch
contains, so a runner is only as safe as the code it accepts.

> **The routing rule in `ci.yml` is not a security boundary.** A pull
> request runs the workflow files *from that pull request*. Anyone who
> opens a pull request from a fork can edit `.github/workflows/` in it and
> add a job with `runs-on: [self-hosted, halpradio-mac]`. That job lands on
> your runner whatever `ci.yml` on `main` says. Only the approval setting
> below stops it.

The setup has five layers:

1. **Outside pull requests wait for your approval.** With "Require approval
   for all external contributors" set, no workflow from a fork runs until a
   maintainer clicks *Approve and run*. **Before approving, open the
   pull request's Files tab and check every change under `.github/`.** A
   pull request that touches workflows, or anything a workflow runs, can
   target this runner.
2. **Accidental routing is avoided.** In `.github/workflows/ci.yml`, the
   macOS job uses the self-hosted runner only for pushes to `main`, the
   weekly scheduled run, and pull requests from branches in this repository
   that Dependabot did not open. Approved fork pull requests and Dependabot
   updates use GitHub-hosted runners for that job, unless the pull request
   itself changes this rule (see layer 1).
3. **Every job runs in a new VM.** `scripts/mac-runner/ephemeral-runner.sh`
   clones a fresh [Tart](https://tart.run) macOS VM for each job and deletes
   it afterwards. Jobs never run on the host, and nothing carries over
   from one job to the next.
4. **The VM holds no lasting credential.** Each VM gets a single-use JIT
   runner config, which GitHub invalidates after one job. The token that
   creates these configs stays on the host. The runner tarball is shared
   into the VM read-only. The VM cannot see your home folder.
5. **The runner is checked before use.** The runner download must match the
   SHA-256 you set in `RUNNER_SHA256`, or the script deletes it and stops.

**What this does not cover:**

- **Network access.** By default the VM can reach your home network. See
  [Network isolation](#network-isolation).
- **The job token.** A job running on the VM still receives that job's
  `GITHUB_TOKEN` (`contents: read`, `pull-requests: write`).

Do not run the runner directly on a machine that holds other credentials,
such as an agent server.

**Do not use `config.sh` or `svc.sh`.** GitHub's standard instructions on
the "New self-hosted runner" page install a long-lived runner on the host,
outside any VM. This setup never needs them: the script registers a
single-use runner inside each VM. If you already installed one that way,
see [Removing a host runner](#removing-a-host-runner).

## One-time setup

On the Mac (Apple Silicon, macOS 14 or later, about 60 GB free). Check the
chip first; Tart needs Apple Silicon:

```bash
sysctl -n machdep.cpu.brand_string
```

Install the GitHub CLI and Tart:

```bash
brew install gh
brew install cirruslabs/cli/tart
```

If `brew install cirruslabs/cli/tart` fails with a formula error from the
tap, install Tart from its GitHub release instead. Verify the archive
against the release's checksum file before you extract it:

```bash
mkdir -p ~/tart-install && cd ~/tart-install
gh release download --repo cirruslabs/tart --pattern 'tart.tar.gz' --pattern '*checksums.txt'
grep ' tart.tar.gz$' ./*checksums.txt | shasum -a 256 -c
tar -xzf tart.tar.gz
mv tart.app ~/Applications/
ln -sf ~/Applications/tart.app/Contents/MacOS/tart /opt/homebrew/bin/tart
tart --version
```

If your Homebrew lives in `/usr/local` (an Intel-era install), link into
`/usr/local/bin` instead.

Create a fine-grained personal access token at
<https://github.com/settings/personal-access-tokens/new>:

- **Resource owner:** `halpworld`
- **Repository access:** only `halpworld/halpradio`
- **Permissions:** Repository → Administration → Read and write (the only
  one needed; it lets the script create JIT runner configs)
- **Expiry:** short, for example 90 days

In the repository settings on GitHub:

1. **Actions → General → Approval for running fork pull request workflows:**
   choose "Require approval for all external contributors". This setting is
   **required**: it is the only thing that keeps code from forks off the
   runner (see [Security model](#security-model)).
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

## Removing a host runner

If you followed GitHub's own "New self-hosted runner" steps (`config.sh`,
then `svc.sh install`), a persistent runner is running on the host itself.
Remove it before using this setup. From the runner folder:

```bash
./svc.sh stop
./svc.sh uninstall
./config.sh remove --token "$(gh api -X POST repos/halpworld/halpradio/actions/runners/remove-token --jq .token)"
```

Then check that no runners are left under **Settings → Actions → Runners**,
and delete the runner folder.
