# pve Usage

`pve` is a personal HomeLab Proxmox VE CLI for daily VM/QEMU and LXC
operations.

## Help Command Order

`pve --help` groups commands into categories that follow daily HomeLab use,
from overview and health checks to local tools:

| Category | Commands, in display order |
| --- | --- |
| Dashboard | `status`, `check`, `doctor` |
| Guests | `guest`, `vm`, `lxc` |
| Infrastructure | `node`, `task`, `storage`, `backup`, `network`, `firewall` |
| Local | `config`, `version` |

Nested help keeps list/detail queries first, then lifecycle
operations, configuration and maintenance, backup/recovery, and destructive
operations. VM/LXC lifecycle commands use `start`, `shutdown`, `reboot`, `stop`
in that order. VM guest-agent tools follow lifecycle commands, and cloud-init
is next to `config`. `delete` is the last VM/LXC operation; snapshot help lists
`ls`, `create`, `delete`, `rollback`, placing rollback last.

Config help lists `ls`, `show`, `add`, `set`, `use`, `help` to prioritize
inspection before configuration changes. The built-in `help` command remains
last at each level.

## Configuration

The default config file is `~/.config/pve/config.yaml`. Use `--config PATH`
to select another file. After the rename from `pvectl`, move an existing
config file to the new path or select it explicitly; the old directory is
not searched or migrated automatically.

For a typical HomeLab setup, add a profile interactively and run a diagnostic
check:

```bash
pve config add
pve config ls
pve config show
pve doctor
```

### List and Show Profiles

`config ls` prints a table with only the profile name and endpoint, sorted by
profile name. `config show` displays the current profile by default. Pass a name
or use the global `--profile` flag to select a profile, or use `--all` to display
every profile:

```bash
pve config ls
pve config show
pve config show lab
pve --profile lab config show
pve config show --all
```

Profile selectors cannot be combined: a positional name together with
`--profile`, or `--all` together with either selector, produces an error.
`config ls` and `config show` always display tables. Show renders a two-column
`FIELD` / `VALUE` table for each profile, separated by a blank line when showing
multiple profiles. Its fields are `Profile`, `Current`, `Endpoint`, `Token ID`,
`Token secret`, `Token secret env`, `Skip TLS verify`, `Timeout`, and
`Default output`.

Each non-empty `Token secret` is a summary. Values longer than six Unicode
characters show the first three and last three characters with `*****` between
them, such as `dd1*****ef4`; values of six characters or fewer show only `*****`.
This display does not change the stored token or authentication. `Token secret
env` displays the configured environment variable name without reading its value.

The final line of `config show` gives the absolute config-file path after
expanding `~` and environment variables. Line breaks in file names are escaped
to keep the path on one line:

```text
Config file: /home/user/.config/pve/config.yaml
```

### Add Profiles Interactively

`config add` prompts for the profile name, PVE API endpoint, token ID, a plaintext
token or environment variable reference, and whether to skip TLS verification.
Plaintext token input is hidden in the terminal. The default timeout is `30s`,
the default output is `table`, and TLS verification is enabled. Global
`--api-timeout` and `--insecure` supply defaults when provided.

The profile name defaults to global `--profile` or `home`. The wizard asks whether to
make the added profile current. The default answer is yes when no current
profile is configured and no otherwise. An existing profile of the same name
is never overwritten. Ending input cancels without writing changes. Prompts
and setup results go to stderr.

If the file is missing, `config ls` and `config show` report that it does not
exist and offer the same guided initialization in a terminal. With nonterminal
stdin, they display a `config add` / `config set` setup hint and exit
successfully without waiting for input. Declining or ending input also exits
successfully. Stdout stays empty during setup; run `config ls` or `config show`
again after creation. Initialization never overwrites a file that appeared while
entering the values. Invalid YAML and file-access failures still produce errors.

### Set Profiles Noninteractively

`config set NAME` creates or replaces a named profile using flags. Connection
writes require `--endpoint`, `--token-id`, and at least one of `--token-secret`
or `--token-secret-env`:

```bash
pve config set lab \
  --endpoint https://pve-lab.lan:8006/api2/json \
  --token-id 'automation@pve!pve' \
  --token-secret-env PVE_LAB_TOKEN_SECRET \
  --timeout 30s \
  --default-output table
```

Profiles default to timeout `30s` and output `table`. `config set` never
changes which profile is current; the first profile written into an empty
config becomes current automatically.

### Switch Profiles

```bash
pve config use home
```

`config use NAME` only selects the current profile. It requires the named
profile to exist and never rewrites its connection settings.

`config` exposes only `ls`, `show`, `add`, `set`, and `use`.

### Stored Configuration

Config schema:

```yaml
current_profile: home
profiles:
  home:
    endpoint: https://pve.lan:8006/api2/json
    token_id: automation@pve!pve
    token_secret: your-token-secret
    insecure_skip_verify: true
    timeout: 30s
    default_output: table
```

`token_secret` stores the token value directly in plaintext. It can be set by
editing the YAML file, entering it in `config add`, or passing `--token-secret`
to `config set NAME`.

For environment-based configuration, replace `token_secret` with
`token_secret_env: PVE_HOME_TOKEN_SECRET` in YAML, or pass
`--token-secret-env PVE_HOME_TOKEN_SECRET` instead of `--token-secret`:

```bash
export PVE_HOME_TOKEN_SECRET='your-token-secret'
```

A non-empty `token_secret` takes precedence over `token_secret_env`; the
environment variable is read only when the plaintext value is empty or absent.
Environment variable names are user-defined. Config files are saved with
permissions `0600` on Unix, including when replacing an existing file.
Doctor reports the credential source without printing its value.

## Diagnostics

```bash
pve doctor
pve doctor --offline
pve doctor --node pve1
pve doctor -o json
```

`doctor` checks the config path, config file, YAML parsing, selected profile,
required profile fields, plaintext token or token-secret environment variable, timeout,
default output, endpoint shape, Proxmox API connectivity, and node listing
permission. It never prints token secret values.

Use `--offline` to skip Proxmox API calls. Use `--node` to verify a specific
node exists during online checks. Doctor rows are written to stdout and support
the same `table`, `json`, and `yaml` output formats as resource commands.

## Version

```bash
pve version
pve version -o json
pve --version
```

`pve version` writes build and runtime metadata. It does not read the config
file or connect to Proxmox VE. `pve --version` keeps the compact CLI version
print from the underlying CLI framework.

## Daily Commands

### HomeLab Status and Health

`status` answers "is my PVE basically fine right now?" with a compact overview.
It tolerates partial failures: sections that cannot be queried are reported in
`issues` instead of failing the whole command.

```bash
pve status
pve status -o json
```

`check` reports HomeLab health with a per-check exit-code contract: `fail`
makes the command exit non-zero, `warn` does not. With `--strict`, warnings
also fail. This makes it suitable for cron, systemd timers, and automation.

```bash
pve check
pve check --node pve1
pve check --storage-warn 85 --storage-fail 95
pve check --backup-tag backup --backup-max-age 36h
pve check --strict
```

The backup coverage check only runs when both `--backup-tag` and
`--backup-max-age` are set; guests without that tag are never judged. An
unknown `--node` fails the check instead of reporting an empty green run, and
backup storages that cannot be queried report `backup status unavailable`
rather than `no backup found`.

`doctor` stays separate: it checks whether `pve` itself works, not whether
the HomeLab is healthy.

### Nodes

```bash
pve node ls
pve node get pve1
pve node get pve1 -o json
```

`node get` adds PVE version, kernel, load average, and CPU details on top of
the `node ls` row data.

### Task Inspection

Tasks are first-class resources. Every mutating command prints its task ID on
stderr; `task` commands let you inspect them afterwards.

```bash
pve task ls
pve task ls --node pve1
pve task ls --type vzdump
pve task ls --status running
pve task ls --limit 20

pve task get UPID:pve1:0000F2A3:00000000:6839F4A1:vzdump:100:root@pam:

pve task log UPID:pve1:0000F2A3:00000000:6839F4A1:vzdump:100:root@pam:
pve task log UPID:pve1:0000F2A3:00000000:6839F4A1:vzdump:100:root@pam: --tail 100

pve task wait UPID:pve1:0000F2A3:00000000:6839F4A1:vzdump:100:root@pam: --wait-timeout 20m
```

`task ls` without `--node` aggregates across all nodes and tolerates nodes
that fail to answer as long as one succeeds. Known status values are
`running`, `ok`, `error`, and `unknown`.

### Guest Aggregate View

`guest` aggregates VM/QEMU and LXC guests and hosts the bulk lifecycle
operations.

```bash
pve guest ls
pve guest ls --node pve1
pve guest ls --type vm
pve guest ls --type lxc
pve guest ls --status running
pve guest ls --tag infra
pve guest ls --tag docker --tag production --tag-match all
pve guest ls --tag docker --tag production --tag-match any
pve guest get 100
pve guest get 100 --type vm
pve guest get 200 --type lxc
```

Tag values are matched case-insensitively. `--tag` is repeatable; the default
`--tag-match all` requires every listed tag, `any` requires at least one.
Proxmox VE stores guest tags as one semicolon-separated string (for example
`infra;production`); `pve` decodes that format before matching.

#### Bulk Guest Operations

```bash
pve guest start --tag lab --dry-run
pve guest start --tag lab
pve guest shutdown --tag infra
pve guest reboot --node pve1 --status running
pve guest stop --tag legacy --force
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
pve vm ls
pve vm ls --node pve1
pve vm get 100
pve vm get 100 --node pve1 -o json
pve vm start 100 --wait
pve vm shutdown 100 --wait
pve vm reboot 100 --wait
pve vm stop 100
```

When `--node` is omitted, `pve` traverses all nodes returned by the cluster
and resolves the VMID automatically.

### LXC

```bash
pve lxc ls
pve lxc ls --node pve1
pve lxc get 200
pve lxc get 200 --node pve1 -o json
pve lxc start 200 --wait
pve lxc shutdown 200 --wait
pve lxc reboot 200 --wait
pve lxc stop 200
```

When `--node` is omitted, `pve` traverses all nodes returned by the cluster
and resolves the CTID automatically.

## Backup Commands

Backup commands are intentionally lightweight. They list backup files on a
specific node/storage and create one-off guest backups.

### List Backups

```bash
pve backup ls --node pve1 --storage backup
pve backup ls --node pve1 --storage backup --vmid 100
pve backup ls --node pve1 --storage backup --type vm
pve backup ls --node pve1 --storage backup --type lxc
pve backup ls --node pve1 --storage backup --latest
pve backup ls --node pve1 --storage backup -o json
```

`backup ls` requires both `--node` and `--storage`. Supported backup types are
`all`, `vm`, and `lxc`. Use `--latest` to keep only the newest backup per
guest.

### Create One-off Guest Backups

```bash
pve vm backup 100 --storage backup --mode snapshot --wait
pve lxc backup 200 --storage backup --mode snapshot --wait
```

Common options:

```bash
pve vm backup 100 \
  --storage backup \
  --mode snapshot \
  --compress zstd \
  --notes-template "{{guestname}}" \
  --bwlimit 102400 \
  --protected \
  --wait
```

Supported modes are `snapshot`, `suspend`, and `stop`. Supported compression
values are `zstd`, `lzo`, `gzip`, and `none`.

When `--node` is omitted, `pve` resolves the VMID/CTID automatically before
triggering the backup. Backup results are written to stdout and include the
task ID; task IDs and wait progress are also written to stderr.

One-off VM/LXC backup restore is in scope as a disaster-recovery workflow:
restoring a vzdump archive into a new, non-existing VMID. Overwriting an
existing VMID is not supported; delete the guest first, then restore.

### Restore a Backup Archive

```bash
pve vm restore backup:backup/vzdump-qemu-100-2026_06_06-00_00_00.vma.zst \
  --node pve1 \
  --vmid 101 \
  --storage local-lvm \
  --wait

pve lxc restore backup:backup/vzdump-lxc-200-2026_06_06-00_00_00.tar.zst \
  --node pve1 \
  --vmid 201 \
  --storage local-lvm \
  --wait
```

`--node` and `--vmid` are required. The restore refuses to run when the target
VMID already exists anywhere in the cluster; there is no `--force` overwrite.
The VMID check must see every node, so it aborts before starting when any
node's guest inventory cannot be queried. Results are written to stdout and
include the task ID; wait progress goes to stderr. When a submitted task
fails during `--wait`, the result row is still written to stdout before the
command exits non-zero. When the archive name encodes a vzdump kind (`vzdump-qemu-` or
`vzdump-lxc-`), the kind must match the command.

`pve` does not manage scheduled backup jobs, prune policies, backup
deletion, PBS datastores, or PBS verification.

## Storage Commands

Storage commands are read-only inventory helpers for storage status and generic
storage content.

### List Storages

```bash
pve storage ls
pve storage ls --node pve1
pve storage ls --content backup
pve storage ls --type dir
pve storage ls --active
pve storage ls --enabled
pve storage ls -o json
```

When `--node` is omitted, `pve` traverses all nodes returned by the cluster
and lists storage status on each node. Use `--content` for a single content
capability such as `backup`, `iso`, `images`, or `vztmpl`. Use `--type` for a
single storage type such as `dir`, `lvmthin`, `nfs`, or `pbs`.

### Storage Usage

```bash
pve storage usage
pve storage usage --node pve1
pve storage usage --content backup
```

`storage usage` is a compact daily-use view of the same data as `storage ls`;
structured output reuses the `StorageRow` schema.

### Show Storage Status

```bash
pve storage get local --node pve1
pve storage get backup --node pve1 -o json
```

`storage get` requires `--node` because the same storage name may be visible on
multiple nodes.

### List Storage Content

```bash
pve storage content ls --node pve1 --storage local
pve storage content ls --node pve1 --storage local --content iso
pve storage content ls --node pve1 --storage backup --content backup
pve storage content ls --node pve1 --storage local-lvm --content images
pve storage content ls --node pve1 --storage backup --vmid 100
pve storage content ls --node pve1 --storage local -o json
```

`storage content ls` shows generic storage contents such as ISO images, LXC
templates, backup files, VM disks, and container root disks. If you only care
about backup files, prefer `backup ls`; it uses backup-specific fields and
supports `--type` and `--latest`.

`pve` does not create, update, delete, upload, download, prune, or otherwise
mutate storages or storage content. It also does not manage PBS datastores.

## Maintenance Commands

### Clone

```bash
pve vm clone 9000 --newid 101 --name app-vm --target pve1 --wait
pve vm clone 9000 --name app-vm --target pve1 --storage local-lvm --full --wait

pve lxc clone 900 --newid 201 --hostname app-lxc --target pve1 --wait
pve lxc clone 900 --hostname app-lxc --target pve1 --storage local-lvm --full --wait
```

Omit `--newid` to let Proxmox allocate the next available VMID/CTID. Clone
results are written to stdout and include `new_vmid`, so scripts can capture
the allocated ID:

```bash
pve vm clone 9000 --name app-vm --target pve1 -o json
```

### Config

```bash
pve vm config 101 --set memory=4096 --set cores=4 --wait
pve lxc config 201 --set memory=2048 --set cores=2 --wait
```

`config` passes generic `key=value` options to the Proxmox guest config API.

### Resize

```bash
pve vm resize 101 --disk scsi0 --size +20G --wait
pve lxc resize 201 --disk rootfs --size +10G --wait
```

### Migrate

```bash
pve vm migrate 101 --target pve2 --online --wait
pve lxc migrate 201 --target pve2 --online --wait
```

## Snapshot Commands

```bash
pve vm snapshot ls 101
pve vm snapshot create 101 before-upgrade --wait
pve vm snapshot delete 101 before-upgrade --wait

pve lxc snapshot ls 201
pve lxc snapshot create 201 before-upgrade --wait
pve lxc snapshot delete 201 before-upgrade --wait
```

Snapshot rollback and snapshot delete are dangerous operations and are
documented separately below.

## VM Agent Commands (QEMU Guest Agent)

Agent commands only work on VMs with the QEMU guest agent installed and
enabled. LXC containers do not expose the agent API. The first release
intentionally limits itself to `ping`, `network`, and `exec`.

```bash
pve vm agent ping 100 --node pve1
pve vm agent network 100 -o json
pve vm agent exec 100 --node pve1 -- /usr/bin/uname -a
```

Like other VMID-oriented commands, `--node` is optional here: when omitted,
`pve` locates the VM across the cluster first.

`vm agent network` answers "which IP did this cloned VM get?". `vm agent exec`
takes `executable + argv` after `--` and never wraps the command in a shell
implicitly; choose `/bin/sh -c ...` yourself if you want shell semantics. The
guest command's exit code is preserved in the structured result, and the
command exits non-zero when the guest command failed.

## VM Cloud-init Commands

Cloud-init commands use PVE's native cloud-init configuration; `pve` never
builds ISOs itself. As with the agent commands, `--node` is optional and an
omitted node is resolved by locating the VM across the cluster.

```bash
pve vm cloud-init get 100 -o json

pve vm cloud-init set 100 \
  --user debian \
  --ssh-key-file ~/.ssh/id_ed25519.pub \
  --ipconfig0 ip=dhcp \
  --nameserver 192.168.2.67 \
  --searchdomain lan \
  --wait

pve vm cloud-init regenerate 100
```

Passwords are never accepted as a command-line flag; pass the environment
variable name instead so the secret stays out of shell history and process
lists:

```bash
export VM_PASSWORD=...
pve vm cloud-init set 100 --password-env VM_PASSWORD
```

`cloud-init get` never echoes the password; it only reports
`password_configured`. `cloud-init regenerate` regenerates the cloud-init
image so the next boot picks up pending changes. `--ssh-key-file` expects one
full OpenSSH public key per line; each line is preserved as-is, and empty or
`#` comment lines are skipped.

## Network Commands (read-only)

```bash
pve network ls --node pve1
pve network ls --type bridge
pve network ls --active
pve network get vmbr0 --node pve1
```

Without `--node`, `network ls` aggregates across all nodes with the usual
partial-success behavior. Network mutation (create/update/delete/apply) is a
non-goal because a remote mistake can take down the whole node.

## Firewall Commands (read-only)

```bash
pve firewall status --node pve1
pve firewall ls --node pve1

pve firewall status --node pve1 --scope vm --vmid 100
pve firewall ls --node pve1 --scope vm --vmid 100

pve firewall ls --node pve1 --scope lxc --vmid 200
```

`--scope node` is the default and requires `--node`. For `vm` and `lxc`
scopes, `--vmid` is required and `--node` is optional: when omitted, `pve`
locates the guest across the cluster like other VMID-oriented commands.
Firewall rule mutation is a non-goal.

## Dangerous Operations

### Delete

Delete commands require a local confirmation prompt unless `--force` is passed:

```bash
pve vm delete 101
pve lxc delete 201
```

The prompt requires typing the exact VMID/CTID. The `--force` flag only skips
this local prompt; it is not passed to the Proxmox LXC delete API.

Use `--wait` when scripts need completion status:

```bash
pve vm delete 101 --force --wait
pve lxc delete 201 --force --wait
```

### Snapshot Rollback

Snapshot rollback commands require typing the exact snapshot name unless
`--force` is passed:

```bash
pve vm snapshot rollback 101 before-upgrade
pve lxc snapshot rollback 201 before-upgrade
```

The `--force` flag only skips this local prompt. Rollback is an asynchronous
PVE task, so use `--wait` when scripts need completion status:

```bash
pve vm snapshot rollback 101 before-upgrade --force --wait
pve lxc snapshot rollback 201 before-upgrade --force --wait
```

### Snapshot Delete

Snapshot delete commands require typing the exact snapshot name unless
`--force` is passed:

```bash
pve vm snapshot delete 101 before-upgrade
pve lxc snapshot delete 201 before-upgrade
```

Like rollback, delete is an asynchronous PVE task, so use `--wait` when scripts
need completion status.

## Output Formats

Supported output formats are `table`, `json`, and `yaml`. `-o`/`--output` is a
per-command flag on commands that produce structured output; local `config`
commands always print tables.

```bash
pve node ls -o table
pve guest ls -o json
pve vm get 100 -o json
pve lxc get 200 -o yaml
```

Use `table` for interactive use, `json` for scripts and agents, and `yaml` as
an optional human-readable structured format.

JSON and YAML field names and field types are stable within v1.x. Table output
is intended for humans and should not be parsed by scripts. See
[`output-schema.md`](output-schema.md) and
[`compatibility.md`](compatibility.md).

## Scripting Notes

Global flags only control the connection context:

```bash
pve \
  --config ~/.config/pve/config.yaml \
  --profile home \
  --api-timeout 30s \
  --insecure \
  --verbose \
  <resource> <action>
```

Behavior flags belong to the command itself: `-o`/`--output` is only accepted
by commands with structured output, and `--wait`/`--wait-timeout` only by
async operation commands.

Timeouts keep separate roles: global `--api-timeout` bounds PVE API requests,
`--timeout` on `vm agent exec` bounds the guest command, and `--wait-timeout`
bounds async task waits.

Async guest operations support:

```bash
pve vm reboot 100 --wait --wait-timeout 5m
```

Task IDs and wait progress are written to stderr. Command results are written
to stdout.
