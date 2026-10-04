# pvectl

Personal HomeLab Proxmox VE CLI.

`pvectl` wraps the Proxmox VE API through
[`go-proxmox`](https://github.com/luthermonson/go-proxmox) and focuses on
daily VM/QEMU and LXC operations. It is intentionally small: resource-oriented
commands for a personal Proxmox cluster, not a full management platform.

## Install

With Homebrew:

```bash
brew install lz-wang/tap/pvectl
```

From source:

```bash
make build
```

The binary is written to `bin/pvectl`.

Optional local install:

```bash
sudo make install
```

## Configure

For a typical HomeLab setup, one default profile is enough. Create an API token
in Proxmox VE, export the token secret, initialize the profile, then run a
diagnostic check:

```bash
export PVECTL_HOME_TOKEN_SECRET="your-token-secret"

pvectl config init \
  --endpoint https://pve.lan:8006/api2/json \
  --token-id automation@pve!pvectl \
  --token-secret-env PVECTL_HOME_TOKEN_SECRET \
  --insecure

pvectl doctor
```

`pvectl` stores only the environment variable name in
`~/.config/pvectl/config.yaml`; it does not write token secrets to disk.
Use `pvectl config set-profile` when you need more than one profile.

## Daily Usage

```bash
pvectl status
pvectl check
pvectl doctor

pvectl node ls
pvectl version

pvectl guest ls
pvectl guest get 100
pvectl guest ls --status running
pvectl guest ls --tag infra

pvectl task ls
pvectl task log UPID:pve1:0000F2A3:00000000:6839F4A1:vzdump:100:root@pam: --tail 100

pvectl backup ls --node pve1 --storage backup

pvectl storage ls
pvectl storage usage
pvectl storage content ls --node pve1 --storage local

pvectl vm ls
pvectl vm get 100
pvectl vm start 100 --wait
pvectl vm shutdown 100 --wait
pvectl vm backup 100 --storage backup --mode snapshot --wait
pvectl vm restore backup:backup/vzdump-qemu-100.vma.zst --node pve1 --vmid 101 --storage local-lvm --wait
pvectl vm stop 100

pvectl lxc ls
pvectl lxc get 200
pvectl lxc start 200 --wait
pvectl lxc backup 200 --storage backup --mode snapshot --wait
pvectl lxc stop 200
```

Shut down every guest tagged `infra`, with a preview first:

```bash
pvectl guest shutdown --tag infra --dry-run
pvectl guest shutdown --tag infra
```

Check backup coverage for guests tagged `backup` (with a 36h SLA) as part of a
health check:

```bash
pvectl check --backup-tag backup --backup-max-age 36h
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
pvectl guest get 100 -o json
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

To regenerate the Formula locally for an existing release:

```bash
scripts/update-homebrew-formula.sh v1.0.0 /Users/lzwang/projects/homebrew-tap
```

## Non-goals

`pvectl` is not intended to be:

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
