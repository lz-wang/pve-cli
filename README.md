# pve

Personal HomeLab Proxmox VE CLI.

`pve` wraps the Proxmox VE API through
[`go-proxmox`](https://github.com/luthermonson/go-proxmox) and focuses on
daily VM/QEMU and LXC operations. It is intentionally small: resource-oriented
commands for a personal Proxmox cluster, not a full management platform.

## Install

With Homebrew:

```bash
brew install lz-wang/tap/pve
```

From source:

```bash
make build
```

The binary is written to `bin/pve`.

Optional local install:

```bash
sudo make install
```

## Configure

For a typical HomeLab setup, one default profile is enough. Create an API token
in Proxmox VE, then run guided setup in a terminal. It prompts for the profile,
connection settings, and token source, with hidden input for a plaintext token:

```bash
pve config add
pve config ls
pve config show
pve doctor
```

The wizard can store the token directly as plaintext `token_secret` in
`~/.config/pve/config.yaml`, with file permissions `0600` on Unix, or save a
`token_secret_env` reference such as `PVE_HOME_TOKEN_SECRET`. A non-empty
`token_secret` takes precedence when both are configured. `config ls` lists
profile names and endpoints; `config show` displays details with token summaries
and the config-file path. Both offer guided setup when the file is missing.
Use `config add` for another profile or `config update NAME` for noninteractive
configuration. Switch the current profile with `config update NAME --use`.
See [docs/usage.md](docs/usage.md) for flags and examples.

## Daily Usage

```bash
pve status
pve check
pve doctor

pve node ls
pve version

pve guest ls
pve guest get 100
pve guest ls --status running
pve guest ls --tag infra

pve task ls
pve task log UPID:pve1:0000F2A3:00000000:6839F4A1:vzdump:100:root@pam: --tail 100

pve backup ls --node pve1 --storage backup

pve storage ls
pve storage usage
pve storage content ls --node pve1 --storage local

pve vm ls
pve vm get 100
pve vm start 100 --wait
pve vm shutdown 100 --wait
pve vm backup 100 --storage backup --mode snapshot --wait
pve vm restore backup:backup/vzdump-qemu-100.vma.zst --node pve1 --vmid 101 --storage local-lvm --wait
pve vm stop 100

pve lxc ls
pve lxc get 200
pve lxc start 200 --wait
pve lxc backup 200 --storage backup --mode snapshot --wait
pve lxc stop 200
```

Shut down every guest tagged `infra`, with a preview first:

```bash
pve guest shutdown --tag infra --dry-run
pve guest shutdown --tag infra
```

Check backup coverage for guests tagged `backup` (with a 36h SLA) as part of a
health check:

```bash
pve check --backup-tag backup --backup-max-age 36h
```

Use `guest` for aggregate views across VM/QEMU and LXC guests and for bulk
lifecycle operations. Use `vm` and `lxc` for single-guest lifecycle operations.
`network` and `firewall` are read-only inventory commands.

Backup commands are intentionally limited to listing backup files, creating
one-off guest backups, and restoring a backup archive into a new,
non-existing VMID.

Storage commands are read-only inventory helpers.

Default output is `table` for humans. Use `-o json` for scripts:

```bash
pve guest get 100 -o json
```

JSON and YAML fields are stable within v1.x. See [docs/usage.md](docs/usage.md)
for clone, config, resize, migrate, snapshot, delete, output formats, and
scripting details. See [docs/output-schema.md](docs/output-schema.md) and
[docs/compatibility.md](docs/compatibility.md) for the structured output and
compatibility contracts.

## Homebrew Release

Tag releases publish GitHub Release assets and then update
`lz-wang/homebrew-tap`. Configure the `HOMEBREW_TAP_TOKEN` repository secret in
`lz-wang/pvectl` with permission to push to the tap repository.

To regenerate `Formula/pve.rb` locally, set `PVE_RELEASE_TAG` to a release tag
that publishes `pve-*` assets and run:

```bash
scripts/update-homebrew-formula.sh "$PVE_RELEASE_TAG" /Users/lzwang/projects/homebrew-tap
```

## Non-goals

`pve` is not intended to be:

- a Web UI
- a server mode or HTTP API
- a multi-user control plane
- an RBAC, audit, billing, or policy platform
- a replacement for the Proxmox VE Web UI

Feature-scope non-goals:

- scheduled backup job management
- backup prune policy management
- PBS datastore administration
- PBS verification/prune administration
- HA/Ceph/SDN full management
- arbitrary Proxmox REST API passthrough

One-off VM/LXC backup restore (vzdump archive into a new, non-existing VMID) is
in scope as a disaster-recovery workflow. See
[docs/compatibility.md](docs/compatibility.md).
