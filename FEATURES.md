# pve Features

`pve` is a small, resource-oriented Proxmox VE CLI for personal HomeLab
operations. It wraps the Proxmox VE API through `go-proxmox` and focuses on
daily VM/QEMU and LXC workflows.

## Global CLI Capabilities

- Uses the `pve` executable for local builds, installs, and release packages;
  Homebrew installs it with `brew install lz-wang/tap/pve`.
- Supports `table`, `json`, and `yaml` output via `--output` or `-o`.
- Supports profile selection with `--profile`.
- Supports custom config paths with `--config`.
- Supports API timeout override with `--timeout`.
- Supports task waiting with `--wait` and `--wait-timeout` on async operations.
- Supports TLS verification override with `--insecure`.
- Keeps command results on stdout.
- Keeps task IDs, wait progress, and logs on stderr.

## Configuration

`pve config` manages local YAML configuration and profile selection.

The default config path is `~/.config/pve/config.yaml`. Existing config files
can be moved there or selected with `--config`; no old-path fallback or
automatic migration is performed. Token-secret environment variable names
are user-defined; documentation uses `PVE_*` examples.

- `config init` initializes a default HomeLab profile.
- `config set-profile NAME` creates or updates a named profile.
- `config use-profile NAME` switches the current profile.
- `config current-profile` prints the active profile name.
- `config view` prints the current config file, including plaintext tokens.
  When the file is missing, it reports the missing file and offers interactive
  initialization with hidden token input in a terminal. Declining or ending input
  leaves the file uncreated; nonterminal input receives a setup hint without
  waiting. These cases exit successfully. Setup messages go to stderr and never
  overwrite an existing file.

Config stores the Proxmox endpoint, token ID, TLS behavior, timeout, and default
output format. Tokens can be stored directly as plaintext `token_secret` via
`--token-secret`, or referenced by `token_secret_env` via `--token-secret-env`.
`config init` and `config set-profile` require at least one token source; a
non-empty plaintext value takes precedence over the environment reference.
Config files are saved with permissions `0600` on Unix, including existing files.

## Diagnostics

`pve doctor` validates the local configuration and, unless `--offline` is
used, verifies Proxmox API connectivity.

Checks include:

- config path and file existence
- YAML parsing
- selected profile
- required profile fields
- plaintext token or token-secret environment variable presence
- timeout and default output settings
- endpoint shape and TLS mode
- API connectivity
- node listing permission
- optional specific node validation with `--node`

Doctor emits structured diagnostic rows and avoids printing token secrets.

`doctor` checks whether `pve` itself can work; `check` (below) reports
whether the HomeLab is healthy.

## HomeLab Status

`pve status` aggregates a compact HomeLab overview:

- node summary (total/online/offline with per-node rows)
- guest summary (total, running, stopped, VM and LXC counts)
- storage summary (total, active, per-storage rows)
- backup summary (count, latest backup age across backup-capable storages;
  shared storages are queried on one node and counted once)
- per-component `issues` for queries that failed, instead of failing the whole
  command

## HomeLab Health Check

`pve check` inspects HomeLab health with cron-friendly exit semantics:

- offline nodes report `fail`
- inactive storages report `fail`, disabled storages report `warn`
- storage usage over `--storage-warn` (default 85%) reports `warn`, over
  `--storage-fail` (default 95%) reports `fail`
- optional backup coverage check with `--backup-tag` and `--backup-max-age`
  for guests carrying that tag; unqueryable backup storages report
  `backup status unavailable` instead of `no backup found`
- an unknown `--node` reports `fail` instead of an empty green run
- `fail` exits non-zero; `--strict` makes warnings exit non-zero too

## Version

`pve version` prints build and runtime metadata without reading config or
connecting to Proxmox VE.

It supports the normal output formats:

- `pve version`
- `pve version -o json`
- `pve version -o yaml`

## Nodes

`pve node ls` lists Proxmox VE nodes.

The node output includes status, CPU, memory, disk, and uptime fields.

`pve node get NODE` shows node details: status, CPU, memory, disk, uptime,
PVE version, kernel version, load average, and CPU model/cores/sockets.

## Task Inspection

PVE tasks are first-class resources:

- `task ls` lists recent tasks; without `--node` it aggregates across nodes
  with partial-success behavior
- `task ls --type vzdump`, `task ls --status running`, `task ls --limit N`
- `task get UPID` shows one task's status
- `task log UPID` and `task log UPID --tail N` show task log output
- `task wait UPID --wait-timeout DURATION` blocks until completion

Known task status values: `running`, `ok`, `error`, `unknown`.

## Guest Aggregate View

`pve guest` aggregates VM/QEMU and LXC guests.

- `guest ls` lists all guests.
- `guest ls --node NODE` filters by node.
- `guest ls --type vm` filters to VMs.
- `guest ls --type lxc` filters to containers.
- `guest ls --status running` filters by guest status.
- `guest ls --tag TAG` filters by tag; repeatable, with `--tag-match all|any`
  (default `all`). Proxmox VE stores guest tags as one `;`-separated string;
  `pve` decodes that format before matching.
- `guest get VMID` resolves and shows a guest by ID.
- `guest get VMID --type vm` or `--type lxc` disambiguates duplicate IDs.

Guest output includes kind, VMID/CTID, name, node, status, CPU, memory, disk,
uptime, and tags fields where available from Proxmox VE.

### Bulk Guest Operations

`guest start`, `guest shutdown`, `guest reboot`, and `guest stop` operate on
every guest matching a selection:

- selection requires at least one of `--node`, `--status`, or `--tag`
- selection is the input to mutations and fails closed: when any node cannot
  be queried, the operation is refused instead of running on a partial
  cluster view
- `--type all|vm|lxc` narrows the guest kind
- `--dry-run` prints the affected guests and exits
- operations hitting more than one guest require a local `yes` confirmation;
  `--force` skips it
- `--jobs N` (default 2) bounds concurrency
- `--wait`/`--wait-timeout` wait for each guest's task
- one failing guest never aborts the run; per-guest results are written to
  stdout as `BulkGuestResult` rows, progress goes to stderr, and the command
  exits non-zero only when at least one guest failed

## VM/QEMU Management

`pve vm` manages QEMU virtual machines.

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

`pve lxc` mirrors the VM command shape for LXC containers.

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
pve vm config 101 --set memory=4096 --set cores=4 --wait
pve lxc config 201 --set memory=2048 --set cores=2 --wait
```

## Resize

`vm resize` and `lxc resize` resize a guest disk.

Examples:

```bash
pve vm resize 101 --disk scsi0 --size +20G --wait
pve lxc resize 201 --disk rootfs --size +10G --wait
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
- `vm snapshot delete VMID SNAPNAME`
- `lxc snapshot ls CTID`
- `lxc snapshot create CTID SNAPNAME`
- `lxc snapshot rollback CTID SNAPNAME`
- `lxc snapshot delete CTID SNAPNAME`

Rollback and delete are treated as dangerous operations and require local
confirmation unless `--force` is passed.

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

`vm restore` and `lxc restore` recover a vzdump backup archive into a new,
non-existing VMID/CTID:

- `--node` and `--vmid` are required
- `--storage` optionally selects target storage
- restores refuse existing VMIDs; there is no overwrite flag
- the VMID preflight must see every node; if any node's guest inventory
  cannot be queried, the restore aborts before starting
- when a submitted task fails during `--wait`, the result row (including the
  task ID) is still written to stdout before the command exits non-zero
- archive kind (`vzdump-qemu-`/`vzdump-lxc-`) must match the command when the
  archive name encodes it

## VM QEMU Guest Agent (VM only)

`vm agent` queries the QEMU guest agent. LXC containers do not expose it.

- `--node` is optional for all agent commands; when omitted, the VM is
  located across the cluster, matching other VMID-oriented commands
- `vm agent ping VMID` verifies the agent answers
- `vm agent network VMID` lists interfaces and addresses as seen inside the
  guest (answers "which IP did this cloned VM get?")
- `vm agent exec VMID -- COMMAND [ARG...]` runs executable+argv in the guest
  without an implicit shell; supports `--input` for stdin data and `--timeout`
  for the wait bound; the guest exit code is preserved and reflected in the
  command exit status

File write/read, fs freeze, and password reset are intentionally out of scope.

## VM Cloud-init

Cloud-init commands use PVE-native cloud-init configuration.

- `--node` is optional for all cloud-init commands; when omitted, the VM is
  located across the cluster
- `vm cloud-init get VMID` shows the normalized cloud-init config; the password
  is never echoed, only `password_configured`
- `vm cloud-init set VMID` updates `--user`, `--ssh-key-file`,
  `--ipconfig0..3`, `--nameserver`, `--searchdomain`, and `--password-env`
- passwords are only accepted through an environment variable named by
  `--password-env`, never as a flag value
- `vm cloud-init update VMID` regenerates the cloud-init image so the next
  boot picks up pending changes

## Network Inventory (read-only)

`network` inspects node network interfaces.

- `network ls --node NODE`
- `network ls --type bridge`
- `network ls --active`
- `network ls` without `--node` aggregates across nodes with partial success
- `network get IFACE --node NODE`

Network mutation (create/update/delete/apply/reload) is a non-goal.

## Firewall Inventory (read-only)

`firewall` inspects firewall status and rules.

- `firewall status --node NODE`
- `firewall ls --node NODE`
- `firewall status --node NODE --type vm --vmid VMID`
- `firewall ls --node NODE --type vm --vmid VMID`
- `firewall ls --node NODE --type lxc --vmid CTID`

`--type node` is the default. Firewall rule mutation is a non-goal.

## Storage Inventory

Storage commands are read-only.

`storage ls` lists storage status:

- `storage ls`
- `storage ls --node NODE`
- `storage ls --content backup`
- `storage ls --type dir`
- `storage ls --active`
- `storage ls --enabled`

`storage usage` is a compact daily-use view reusing the `StorageRow` schema.

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
- `vm snapshot delete`
- `lxc snapshot delete`
- bulk `guest start/shutdown/reboot/stop` hitting more than one guest

Delete prompts require typing the exact VMID/CTID unless `--force` is passed.
Snapshot rollback and delete prompts require typing the exact snapshot name
unless `--force` is passed. Bulk operations require typing `yes` unless
`--force` is passed.

`--force` skips the local confirmation prompt.
