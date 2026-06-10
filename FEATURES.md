# pvectl Features

`pvectl` is a small, resource-oriented Proxmox VE CLI for personal HomeLab
operations. It wraps the Proxmox VE API through `go-proxmox` and focuses on
daily VM/QEMU and LXC workflows.

## Global CLI Capabilities

- Supports `table`, `json`, and `yaml` output via `--output` or `-o`.
- Supports profile selection with `--profile`.
- Supports custom config paths with `--config`.
- Supports API timeout override with `--timeout`.
- Supports task waiting with `--wait` and `--wait-timeout` on async operations.
- Supports TLS verification override with `--insecure`.
- Keeps command results on stdout.
- Keeps task IDs, wait progress, and logs on stderr.

## Configuration

`pvectl config` manages local YAML configuration and profile selection.

- `config init` initializes a default HomeLab profile.
- `config set-profile NAME` creates or updates a named profile.
- `config use-profile NAME` switches the current profile.
- `config current-profile` prints the active profile name.
- `config view` prints the current config file.

Config stores the Proxmox endpoint, token ID, token-secret environment variable
name, TLS behavior, timeout, and default output format. It intentionally stores
only `token_secret_env`, not the token secret value.

## Diagnostics

`pvectl doctor` validates the local configuration and, unless `--offline` is
used, verifies Proxmox API connectivity.

Checks include:

- config path and file existence
- YAML parsing
- selected profile
- required profile fields
- token-secret environment variable presence
- timeout and default output settings
- endpoint shape and TLS mode
- API connectivity
- node listing permission
- optional specific node validation with `--node`

Doctor emits structured diagnostic rows and avoids printing token secrets.

## Version

`pvectl version` prints build and runtime metadata without reading config or
connecting to Proxmox VE.

It supports the normal output formats:

- `pvectl version`
- `pvectl version -o json`
- `pvectl version -o yaml`

## Nodes

`pvectl node ls` lists Proxmox VE nodes.

The node output includes status, CPU, memory, disk, and uptime fields.

## Guest Aggregate View

`pvectl guest` is a read-only aggregate view across VM/QEMU and LXC guests.

- `guest ls` lists all guests.
- `guest ls --node NODE` filters by node.
- `guest ls --type vm` filters to VMs.
- `guest ls --type lxc` filters to containers.
- `guest ls --status running` filters by guest status.
- `guest get VMID` resolves and shows a guest by ID.
- `guest get VMID --type vm` or `--type lxc` disambiguates duplicate IDs.

Guest output includes kind, VMID/CTID, name, node, status, CPU, memory, disk,
and uptime fields where available from Proxmox VE.

## VM/QEMU Management

`pvectl vm` manages QEMU virtual machines.

Read operations:

- `vm ls`
- `vm ls --node NODE`
- `vm get VMID`
- `vm get VMID --node NODE`

Lifecycle operations:

- `vm start VMID`
- `vm shutdown VMID`
- `vm stop VMID`
- `vm reboot VMID`

Maintenance operations:

- `vm clone SOURCE_VMID --name NAME --target NODE`
- `vm config VMID --set key=value`
- `vm migrate VMID --target NODE`
- `vm resize VMID --disk DISK --size SIZE`
- `vm backup VMID --storage STORAGE`
- `vm delete VMID`

When `--node` is omitted, the tool can resolve the VM location by traversing
cluster nodes.

## LXC Management

`pvectl lxc` mirrors the VM command shape for LXC containers.

Read operations:

- `lxc ls`
- `lxc ls --node NODE`
- `lxc get CTID`
- `lxc get CTID --node NODE`

Lifecycle operations:

- `lxc start CTID`
- `lxc shutdown CTID`
- `lxc stop CTID`
- `lxc reboot CTID`

Maintenance operations:

- `lxc clone SOURCE_CTID --hostname HOSTNAME --target NODE`
- `lxc config CTID --set key=value`
- `lxc migrate CTID --target NODE`
- `lxc resize CTID --disk DISK --size SIZE`
- `lxc backup CTID --storage STORAGE`
- `lxc delete CTID`

When `--node` is omitted, the tool can resolve the container location by
traversing cluster nodes.

## Clone

VM and LXC clone commands create a new guest from an existing guest.

Common options:

- `--node` selects the source node.
- `--newid` sets the new VMID/CTID; omitting it lets Proxmox allocate one.
- `--target` selects the target node and is required.
- `--storage` selects target storage.
- `--full` requests a full clone.
- `--pool` sets the target resource pool.
- `--snapname` clones from a snapshot.
- `--description` sets a description.
- `--wait` waits for task completion.
- `--wait-timeout` sets task wait timeout.
- `-o json` makes scripts able to capture `new_vmid`.

VM-specific options:

- `--name`
- `--format`

LXC-specific option:

- `--hostname`

## Guest Config Updates

`vm config` and `lxc config` pass generic `key=value` options to the Proxmox
guest config API.

Examples:

```bash
pvectl vm config 101 --set memory=4096 --set cores=4 --wait
pvectl lxc config 201 --set memory=2048 --set cores=2 --wait
```

## Resize

`vm resize` and `lxc resize` resize a guest disk.

Examples:

```bash
pvectl vm resize 101 --disk scsi0 --size +20G --wait
pvectl lxc resize 201 --disk rootfs --size +10G --wait
```

## Migrate

`vm migrate` and `lxc migrate` move a guest to another node.

Supported options:

- `--node` selects the source node.
- `--target` selects the target node and is required.
- `--online` requests online migration.
- `--wait` waits for task completion.

## Snapshots

VM and LXC snapshot commands are grouped under `snapshot`.

- `vm snapshot ls VMID`
- `vm snapshot create VMID SNAPNAME`
- `vm snapshot rollback VMID SNAPNAME`
- `lxc snapshot ls CTID`
- `lxc snapshot create CTID SNAPNAME`
- `lxc snapshot rollback CTID SNAPNAME`

Rollback is treated as a dangerous operation and requires local confirmation
unless `--force` is passed.

## Backups

Backup support is intentionally lightweight.

`backup ls` lists backup files on a specific node and storage:

- `backup ls --node NODE --storage STORAGE`
- `backup ls --node NODE --storage STORAGE --vmid VMID`
- `backup ls --node NODE --storage STORAGE --kind vm`
- `backup ls --node NODE --storage STORAGE --kind lxc`
- `backup ls --node NODE --storage STORAGE --latest`

`vm backup` and `lxc backup` create one-off guest backups:

- `--storage` is required.
- `--mode` supports `snapshot`, `suspend`, and `stop`.
- `--compress` supports `zstd`, `lzo`, `gzip`, and `none`.
- `--notes-template` sets backup notes.
- `--bwlimit` sets bandwidth limit in KiB/s.
- `--protected` accepts `0` or `1`.
- `--wait` waits for completion.

## Storage Inventory

Storage commands are read-only.

`storage ls` lists storage status:

- `storage ls`
- `storage ls --node NODE`
- `storage ls --content backup`
- `storage ls --type dir`
- `storage ls --active`
- `storage ls --enabled`

`storage get STORAGE --node NODE` shows one storage on one node.

`storage content ls` lists generic storage content:

- `storage content ls --node NODE --storage STORAGE`
- `storage content ls --node NODE --storage STORAGE --content iso`
- `storage content ls --node NODE --storage STORAGE --content backup`
- `storage content ls --node NODE --storage STORAGE --vmid VMID`

## Dangerous Operations

The following operations can change or destroy guest state and are intentionally
explicit:

- `vm delete`
- `lxc delete`
- `vm snapshot rollback`
- `lxc snapshot rollback`

Delete prompts require typing the exact VMID/CTID unless `--force` is passed.
Snapshot rollback prompts require typing the exact snapshot name unless
`--force` is passed.

`--force` skips the local confirmation prompt.
