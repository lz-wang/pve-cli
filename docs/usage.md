# pvectl Usage

`pvectl` is a personal HomeLab Proxmox VE CLI for daily VM/QEMU and LXC
operations.

## Configuration

For a typical HomeLab setup, initialize one default profile and run a
diagnostic check:

```bash
export PVECTL_HOME_TOKEN_SECRET="your-token-secret"

pvectl config init \
  --endpoint https://pve.lan:8006/api2/json \
  --token-id automation@pve!pvectl \
  --token-secret-env PVECTL_HOME_TOKEN_SECRET \
  --insecure

pvectl doctor
pvectl config current-profile
pvectl config view
```

Config schema:

```yaml
current_profile: home
profiles:
  home:
    endpoint: https://pve.lan:8006/api2/json
    token_id: automation@pve!pvectl
    token_secret_env: PVECTL_HOME_TOKEN_SECRET
    insecure_skip_verify: true
    timeout: 30s
    default_output: table
```

The token secret is read from the named environment variable at runtime.
`pvectl` does not write token secrets to disk.

Use `config set-profile` and `config use-profile` when you need to manage more
than one profile:

```bash
pvectl config set-profile lab \
  --endpoint https://pve-lab.lan:8006/api2/json \
  --token-id automation@pve!pvectl \
  --token-secret-env PVECTL_LAB_TOKEN_SECRET \
  --timeout 30s \
  --default-output table

pvectl config use-profile lab
```

`config init` defaults to profile name `home`, timeout `30s`, default output
`table`, and sets the initialized profile as current. Use `--name`,
`--overwrite`, or `--no-use` when you need different initialization behavior.

## Diagnostics

```bash
pvectl doctor
pvectl doctor --offline
pvectl doctor --node pve1
pvectl doctor -o json
```

`doctor` checks the config path, config file, YAML parsing, selected profile,
required profile fields, token secret environment variable, timeout,
default output, endpoint shape, Proxmox API connectivity, and node listing
permission. It never prints token secret values.

Use `--offline` to skip Proxmox API calls. Use `--node` to verify a specific
node exists during online checks. Doctor rows are written to stdout and support
the same `table`, `json`, and `yaml` output formats as resource commands.

## Version

```bash
pvectl version
pvectl version -o json
pvectl --version
```

`pvectl version` writes build and runtime metadata. It does not read the config
file or connect to Proxmox VE. `pvectl --version` keeps the compact CLI version
print from the underlying CLI framework.

## Daily Commands

### HomeLab Status and Health

`status` answers "is my PVE basically fine right now?" with a compact overview.
It tolerates partial failures: sections that cannot be queried are reported in
`issues` instead of failing the whole command.

```bash
pvectl status
pvectl status -o json
```

`check` reports HomeLab health with a per-check exit-code contract: `fail`
makes the command exit non-zero, `warn` does not. With `--strict`, warnings
also fail. This makes it suitable for cron, systemd timers, and automation.

```bash
pvectl check
pvectl check --node pve1
pvectl check --storage-warn 85 --storage-fail 95
pvectl check --backup-tag backup --backup-max-age 36h
pvectl check --strict
```

The backup coverage check only runs when both `--backup-tag` and
`--backup-max-age` are set; guests without that tag are never judged.

`doctor` stays separate: it checks whether `pvectl` itself works, not whether
the HomeLab is healthy.

### Nodes

```bash
pvectl node ls
pvectl node get pve1
pvectl node get pve1 -o json
```

`node get` adds PVE version, kernel, load average, and CPU details on top of
the `node ls` row data.

### Task Inspection

Tasks are first-class resources. Every mutating command prints its task ID on
stderr; `task` commands let you inspect them afterwards.

```bash
pvectl task ls
pvectl task ls --node pve1
pvectl task ls --type vzdump
pvectl task ls --status running
pvectl task ls --limit 20

pvectl task get UPID:pve1:0000F2A3:00000000:6839F4A1:vzdump:100:root@pam:

pvectl task log UPID:pve1:0000F2A3:00000000:6839F4A1:vzdump:100:root@pam:
pvectl task log UPID:pve1:0000F2A3:00000000:6839F4A1:vzdump:100:root@pam: --tail 100

pvectl task wait UPID:pve1:0000F2A3:00000000:6839F4A1:vzdump:100:root@pam: --wait-timeout 20m
```

`task ls` without `--node` aggregates across all nodes and tolerates nodes
that fail to answer as long as one succeeds. Known status values are
`running`, `ok`, `error`, and `unknown`.

### Guest Aggregate View

`guest` aggregates VM/QEMU and LXC guests and hosts the bulk lifecycle
operations.

```bash
pvectl guest ls
pvectl guest ls --node pve1
pvectl guest ls --type vm
pvectl guest ls --type lxc
pvectl guest ls --status running
pvectl guest ls --tag infra
pvectl guest ls --tag docker --tag production --tag-match all
pvectl guest ls --tag docker --tag production --tag-match any
pvectl guest get 100
pvectl guest get 100 --type vm
pvectl guest get 200 --type lxc
```

Tag values are matched case-insensitively. `--tag` is repeatable; the default
`--tag-match all` requires every listed tag, `any` requires at least one.

#### Bulk Guest Operations

```bash
pvectl guest start --tag lab --dry-run
pvectl guest start --tag lab
pvectl guest shutdown --tag infra
pvectl guest reboot --node pve1 --status running
pvectl guest stop --tag legacy --force
```

Selection flags: `--node`, `--type all|vm|lxc`, `--status`, `--tag`,
`--tag-match`. At least one of `--node`, `--status`, or `--tag` is required so
a bare command never sweeps the whole cluster.

Safety and execution:

- `--dry-run` prints the affected guests and exits without changing anything.
- Operations hitting more than one guest require a local `yes` confirmation;
  `--force` skips it.
- `--jobs N` (default 2) bounds concurrency; `--wait`/`--wait-timeout` wait
  for each task.
- One failing guest never aborts the run: every per-guest result is written to
  stdout, progress goes to stderr, and the command exits non-zero only if at
  least one guest failed.

Use `guest` for inventory and inspection. Use `vm` and `lxc` commands for
lifecycle and maintenance operations.

`guest ls` defaults to `--type all`. `guest get` defaults to `--type auto` and
searches both VM/QEMU and LXC guests. If both a VM and an LXC exist with the
same ID, specify `--type vm` or `--type lxc`.

### VM/QEMU

```bash
pvectl vm ls
pvectl vm ls --node pve1
pvectl vm get 100
pvectl vm get 100 --node pve1 -o json
pvectl vm start 100 --wait
pvectl vm shutdown 100 --wait
pvectl vm reboot 100 --wait
pvectl vm stop 100
```

When `--node` is omitted, `pvectl` traverses all nodes returned by the cluster
and resolves the VMID automatically.

### LXC

```bash
pvectl lxc ls
pvectl lxc ls --node pve1
pvectl lxc get 200
pvectl lxc get 200 --node pve1 -o json
pvectl lxc start 200 --wait
pvectl lxc shutdown 200 --wait
pvectl lxc reboot 200 --wait
pvectl lxc stop 200
```

When `--node` is omitted, `pvectl` traverses all nodes returned by the cluster
and resolves the CTID automatically.

## Backup Commands

Backup commands are intentionally lightweight. They list backup files on a
specific node/storage and create one-off guest backups.

### List Backups

```bash
pvectl backup ls --node pve1 --storage backup
pvectl backup ls --node pve1 --storage backup --vmid 100
pvectl backup ls --node pve1 --storage backup --kind vm
pvectl backup ls --node pve1 --storage backup --kind lxc
pvectl backup ls --node pve1 --storage backup --latest
pvectl backup ls --node pve1 --storage backup -o json
```

`backup ls` requires both `--node` and `--storage`. Supported backup kinds are
`all`, `vm`, and `lxc`. Use `--latest` to keep only the newest backup per
guest.

### Create One-off Guest Backups

```bash
pvectl vm backup 100 --storage backup --mode snapshot --wait
pvectl lxc backup 200 --storage backup --mode snapshot --wait
```

Common options:

```bash
pvectl vm backup 100 \
  --storage backup \
  --mode snapshot \
  --compress zstd \
  --notes-template "{{guestname}}" \
  --bwlimit 102400 \
  --protected 1 \
  --wait
```

Supported modes are `snapshot`, `suspend`, and `stop`. Supported compression
values are `zstd`, `lzo`, `gzip`, and `none`.

When `--node` is omitted, `pvectl` resolves the VMID/CTID automatically before
triggering the backup. Backup results are written to stdout and include the
task ID; task IDs and wait progress are also written to stderr.

One-off VM/LXC backup restore is in scope as a disaster-recovery workflow:
restoring a vzdump archive into a new, non-existing VMID. Overwriting an
existing VMID is not supported; delete the guest first, then restore.

### Restore a Backup Archive

```bash
pvectl vm restore backup:backup/vzdump-qemu-100-2026_06_06-00_00_00.vma.zst \
  --node pve1 \
  --vmid 101 \
  --storage local-lvm \
  --wait

pvectl lxc restore backup:backup/vzdump-lxc-200-2026_06_06-00_00_00.tar.zst \
  --node pve1 \
  --vmid 201 \
  --storage local-lvm \
  --wait
```

`--node` and `--vmid` are required. The restore refuses to run when the target
VMID already exists anywhere in the cluster; there is no `--force` overwrite.
Results are written to stdout and include the task ID; wait progress goes to
stderr. When the archive name encodes a vzdump kind (`vzdump-qemu-` or
`vzdump-lxc-`), the kind must match the command.

`pvectl` does not manage scheduled backup jobs, prune policies, backup
deletion, PBS datastores, or PBS verification.

## Storage Commands

Storage commands are read-only inventory helpers for storage status and generic
storage content.

### List Storages

```bash
pvectl storage ls
pvectl storage ls --node pve1
pvectl storage ls --content backup
pvectl storage ls --type dir
pvectl storage ls --active
pvectl storage ls --enabled
pvectl storage ls -o json
```

When `--node` is omitted, `pvectl` traverses all nodes returned by the cluster
and lists storage status on each node. Use `--content` for a single content
capability such as `backup`, `iso`, `images`, or `vztmpl`. Use `--type` for a
single storage type such as `dir`, `lvmthin`, `nfs`, or `pbs`.

### Storage Usage

```bash
pvectl storage usage
pvectl storage usage --node pve1
pvectl storage usage --content backup
```

`storage usage` is a compact daily-use view of the same data as `storage ls`;
structured output reuses the `StorageRow` schema.

### Show Storage Status

```bash
pvectl storage get local --node pve1
pvectl storage get backup --node pve1 -o json
```

`storage get` requires `--node` because the same storage name may be visible on
multiple nodes.

### List Storage Content

```bash
pvectl storage content ls --node pve1 --storage local
pvectl storage content ls --node pve1 --storage local --content iso
pvectl storage content ls --node pve1 --storage backup --content backup
pvectl storage content ls --node pve1 --storage local-lvm --content images
pvectl storage content ls --node pve1 --storage backup --vmid 100
pvectl storage content ls --node pve1 --storage local -o json
```

`storage content ls` shows generic storage contents such as ISO images, LXC
templates, backup files, VM disks, and container root disks. If you only care
about backup files, prefer `backup ls`; it uses backup-specific fields and
supports `--kind` and `--latest`.

`pvectl` does not create, update, delete, upload, download, prune, or otherwise
mutate storages or storage content. It also does not manage PBS datastores.

## Maintenance Commands

### Clone

```bash
pvectl vm clone 9000 --newid 101 --name app-vm --target pve1 --wait
pvectl vm clone 9000 --name app-vm --target pve1 --storage local-lvm --full --wait

pvectl lxc clone 900 --newid 201 --hostname app-lxc --target pve1 --wait
pvectl lxc clone 900 --hostname app-lxc --target pve1 --storage local-lvm --full --wait
```

Omit `--newid` to let Proxmox allocate the next available VMID/CTID. Clone
results are written to stdout and include `new_vmid`, so scripts can capture
the allocated ID:

```bash
pvectl vm clone 9000 --name app-vm --target pve1 -o json
```

### Config

```bash
pvectl vm config 101 --set memory=4096 --set cores=4 --wait
pvectl lxc config 201 --set memory=2048 --set cores=2 --wait
```

`config` passes generic `key=value` options to the Proxmox guest config API.

### Resize

```bash
pvectl vm resize 101 --disk scsi0 --size +20G --wait
pvectl lxc resize 201 --disk rootfs --size +10G --wait
```

### Migrate

```bash
pvectl vm migrate 101 --target pve2 --online --wait
pvectl lxc migrate 201 --target pve2 --online --wait
```

## Snapshot Commands

```bash
pvectl vm snapshot ls 101
pvectl vm snapshot create 101 before-upgrade --wait
pvectl vm snapshot delete 101 before-upgrade --wait

pvectl lxc snapshot ls 201
pvectl lxc snapshot create 201 before-upgrade --wait
pvectl lxc snapshot delete 201 before-upgrade --wait
```

Snapshot rollback and snapshot delete are dangerous operations and are
documented separately below.

## VM Agent Commands (QEMU Guest Agent)

Agent commands only work on VMs with the QEMU guest agent installed and
enabled. LXC containers do not expose the agent API. The first release
intentionally limits itself to `ping`, `network`, and `exec`.

```bash
pvectl vm agent ping 100 --node pve1
pvectl vm agent network 100 -o json
pvectl vm agent exec 100 --node pve1 -- /usr/bin/uname -a
```

`vm agent network` answers "which IP did this cloned VM get?". `vm agent exec`
takes `executable + argv` after `--` and never wraps the command in a shell
implicitly; choose `/bin/sh -c ...` yourself if you want shell semantics. The
guest command's exit code is preserved in the structured result, and the
command exits non-zero when the guest command failed.

## VM Cloud-init Commands

Cloud-init commands use PVE's native cloud-init configuration; `pvectl` never
builds ISOs itself.

```bash
pvectl vm cloud-init get 100 -o json

pvectl vm cloud-init set 100 \
  --user debian \
  --ssh-key-file ~/.ssh/id_ed25519.pub \
  --ipconfig0 ip=dhcp \
  --nameserver 192.168.2.67 \
  --searchdomain lan \
  --wait

pvectl vm cloud-init update 100
```

Passwords are never accepted as a command-line flag; pass the environment
variable name instead so the secret stays out of shell history and process
lists:

```bash
export VM_PASSWORD=...
pvectl vm cloud-init set 100 --password-env VM_PASSWORD
```

`cloud-init get` never echoes the password; it only reports
`password_configured`. `cloud-init update` regenerates the cloud-init image so
the next boot picks up pending changes.

## Network Commands (read-only)

```bash
pvectl network ls --node pve1
pvectl network ls --type bridge
pvectl network ls --active
pvectl network get vmbr0 --node pve1
```

Without `--node`, `network ls` aggregates across all nodes with the usual
partial-success behavior. Network mutation (create/update/delete/apply) is a
non-goal because a remote mistake can take down the whole node.

## Firewall Commands (read-only)

```bash
pvectl firewall status --node pve1
pvectl firewall ls --node pve1

pvectl firewall status --node pve1 --type vm --vmid 100
pvectl firewall ls --node pve1 --type vm --vmid 100

pvectl firewall ls --node pve1 --type lxc --vmid 200
```

`--type node` is the default. `--vmid` is required when `--type` is `vm` or
`lxc`. Firewall rule mutation is a non-goal.

## Dangerous Operations

### Delete

Delete commands require a local confirmation prompt unless `--force` is passed:

```bash
pvectl vm delete 101
pvectl lxc delete 201
```

The prompt requires typing the exact VMID/CTID. The `--force` flag only skips
this local prompt; it is not passed to the Proxmox LXC delete API.

Use `--wait` when scripts need completion status:

```bash
pvectl vm delete 101 --force --wait
pvectl lxc delete 201 --force --wait
```

### Snapshot Rollback

Snapshot rollback commands require typing the exact snapshot name unless
`--force` is passed:

```bash
pvectl vm snapshot rollback 101 before-upgrade
pvectl lxc snapshot rollback 201 before-upgrade
```

The `--force` flag only skips this local prompt. Rollback is an asynchronous
PVE task, so use `--wait` when scripts need completion status:

```bash
pvectl vm snapshot rollback 101 before-upgrade --force --wait
pvectl lxc snapshot rollback 201 before-upgrade --force --wait
```

### Snapshot Delete

Snapshot delete commands require typing the exact snapshot name unless
`--force` is passed:

```bash
pvectl vm snapshot delete 101 before-upgrade
pvectl lxc snapshot delete 201 before-upgrade
```

Like rollback, delete is an asynchronous PVE task, so use `--wait` when scripts
need completion status.

## Output Formats

Supported output formats are `table`, `json`, and `yaml`.

```bash
pvectl node ls -o table
pvectl guest ls -o json
pvectl vm get 100 -o json
pvectl lxc get 200 -o yaml
```

Use `table` for interactive use, `json` for scripts and agents, and `yaml` as
an optional human-readable structured format.

JSON and YAML field names and field types are stable within v1.x. Table output
is intended for humans and should not be parsed by scripts. See
[`output-schema.md`](output-schema.md) and
[`compatibility.md`](compatibility.md).

## Scripting Notes

Global flags:

```bash
pvectl \
  --config ~/.config/pvectl/config.yaml \
  --profile home \
  -o json \
  --timeout 30s \
  --insecure \
  --verbose \
  <resource> <action>
```

Async guest operations support:

```bash
pvectl vm reboot 100 --wait --wait-timeout 5m
```

Task IDs and wait progress are written to stderr. Command results are written
to stdout.
