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

### Nodes

```bash
pvectl node ls
```

### Guest Aggregate View

`guest` is a read-only aggregate view across VM/QEMU and LXC guests.

```bash
pvectl guest ls
pvectl guest ls --node pve1
pvectl guest ls --type vm
pvectl guest ls --type lxc
pvectl guest ls --status running
pvectl guest get 100
pvectl guest get 100 --type vm
pvectl guest get 200 --type lxc
```

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

pvectl lxc snapshot ls 201
pvectl lxc snapshot create 201 before-upgrade --wait
```

Snapshot rollback is a dangerous operation and is documented separately below.

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
