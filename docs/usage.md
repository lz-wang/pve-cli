# pve v3 usage

`pve RESOURCE ACTION` manages a personal Proxmox VE HomeLab. The reference
below is generated from the command tree and includes all supported flags.
Use an API token with permissions for each selected operation.

## Profiles and credentials

```bash
pve config add
pve config ls
pve config show --all
pve config use home
```

Noninteractive configuration uses one credential source:

```bash
pve config set home --endpoint https://pve.example:8006/api2/json \
  --token-id 'automation@pve!cli' --token-secret-env PVE_HOME_TOKEN
pve config use home
pve --profile home doctor
```

Alternatively use `--token-secret-file PATH`, or pipe a secret from your secret
manager into `pve config set ... --token-secret-stdin`. Never put token values
in command arguments. Interactive input is hidden. Managed credentials use
0600 files in a 0700 directory and atomic publication; Unix readers require a
private regular file owned by the current user. Windows relies on file ACLs.

```yaml
current_profile: home
profiles:
  home:
    endpoint: https://pve.example:8006/api2/json
    token_id: automation@pve!cli
    token_secret_env: PVE_HOME_TOKEN
    timeout: 30s
    default_output: table
```

A file-based profile uses `token_secret_file: /private/path/token` instead of
the environment reference. Exactly one source is allowed. Unknown YAML fields
are rejected. `config show` displays references without reading secret values,
and ends with the absolute config path. Config writes use 0600 permissions on
Unix. `config set` does not switch an existing current profile. Missing config
files offer interactive setup; interrupted prompts preserve existing files.

## Queries and resource resolution

```bash
pve guest ls --status running --tag infra --sort vmid
pve vm ls --name debian --sort memory --desc
pve vm get debian-test
pve vm get --name 123
pve vm start debian-test --wait
pve storage usage --sort used-percent --desc --min-used-percent 80
```

List names use case-insensitive substring matching; positional names use exact
matching. Numeric positionals mean VMIDs. `--name` on single-target commands
selects exact numeric names; clone's `--name` names its destination. Name
conflicts list candidate node/VMIDs and fail. Unspecified nodes use cluster
inventory. Resolution refuses incomplete inventory instead of selecting a
possibly ambiguous target. Explicit nodes are checked directly.

Guest sorting accepts `vmid/name/node/status/memory/cpu`; memory sorts allocated
memory. Missing metrics stay last in both directions. Storage sorting accepts
`name/node/used-percent/total`, with unknown capacity last. Unknown capacity
never matches a positive usage threshold. Shared storage rows remain per node;
there is no summed cluster-capacity view that double-counts shared disks.

## LXC deployment

```bash
pve lxc template ls --available --node pve1 --section system --name debian
pve lxc template ls --node pve1 --storage local
pve lxc template download EXACT_FILENAME --node pve1 --storage local --wait
pve lxc create 201 --node pve1 --hostname debian-test \
  --template local:vztmpl/EXACT_FILENAME --rootfs local-lvm:8 \
  --memory 2048 --cores 2 --net0 'name=eth0,bridge=vmbr0,ip=dhcp' --wait
pve lxc start 201 --wait
```

Use the exact filename returned by the available index. Only traditional
`system` templates are supported. Download rejects duplicates and verifies the
storage file after waiting. LXC creation checks cluster ID uniqueness, template
readability, active rootdir storage and network bridge before submitting once.
Defaults are 1024 MiB memory, one core, 512 MiB swap, unprivileged and stopped.
`--ssh-key-file` reads public keys; `--password-env` reads a password without
putting it on the command line. Explicit `--unprivileged=false` requests a
privileged container. No automatic retry occurs after task submission.

## VM creation and configuration

```bash
pve vm create 101 --node pve1 --name debian-vm --storage local-lvm \
  --disk-size 32G --memory 4096 --cores 4 --bridge vmbr0 \
  --iso local:iso/debian.iso --wait
pve vm config get 101 -o json
pve vm config set 101 --set memory=8192 --set cores=4 --wait -o json
pve lxc config get 201 -o yaml
pve lxc config set 201 --set memory=2048 -o json
```

VM creation checks ID, node, image storage, bridge and optional ISO. Disk sizes
use positive GiB values such as `32G`. The VM stays stopped; an ISO changes boot
order, but no OS is installed automatically. Without ISO, the empty VM boots
from its disk/network. Cloud-init deployments use a prepared template with
`vm clone` followed by `vm cloud-init set`; an empty disk is not an OS image.

Native configuration appears under a dynamic `config` object. Keys and values
retain the API's original types, including numeric strings returned by PVE.
Credential fields are redacted. Repeated `--set key=value` rejects empty or
duplicate keys. Successful synchronous changes return `succeeded` without a
UPID; asynchronous changes return a task result.

## Disk maintenance and destructive operations

```bash
pve vm disk ls 101
pve vm disk add 101 --slot scsi1 --storage local-lvm --size 20G --wait
pve vm disk remove 101 scsi1
pve vm disk delete 101 unused0 --yes
pve vm resize 101 --disk scsi0 --size +20G --wait
```

`disk ls` distinguishes system, attached, installer and unused devices.
`remove` detaches an attached disk and verifies that its volume is retained in
an unused slot. It refuses `unusedN`. `delete` only accepts an explicit unused
slot, destroys that volume, and verifies absence from storage. Interactive
confirmation includes VMID, slot, node and volume; a changed volume target is
refused before submission. Type `VMID SLOT` exactly.
`--yes` skips only local confirmation. API safety checks still apply. API disk
unlink uses `force=false`; an unused-slot unlink itself performs deletion.

```bash
pve vm snapshot create 101 before-upgrade --wait
pve vm snapshot rollback 101 before-upgrade --dry-run -o json
pve vm snapshot rollback 101 before-upgrade --yes --wait
pve lxc snapshot delete 201 old-snapshot --dry-run
pve vm delete 101 --yes --wait
```

Snapshot rollback/delete preview the exact target, snapshot and impact using
read APIs only. Both verify existence and locks. VM rollback requires a stopped
VM. Confirmation is required outside dry-run; nonterminal dangerous operations
must specify `--yes`. Deletion and rollback are separate from daily lifecycle
commands and can lose data.

## Tasks, bulk operations and automation

```bash
pve guest shutdown --tag infra --dry-run -o json
pve guest shutdown --tag infra --yes --jobs 2 --max-targets 20 --wait -o json
pve task ls --status running
pve task get 'UPID:...'
pve task log 'UPID:...' --tail 100
pve task wait 'UPID:...' --wait-timeout 10m -o json
pve task cancel 'UPID:...' --yes -o json
```

Bulk operations require at least one constraining node/status/tag selector,
a complete selection, 1–8 workers and at most 20 targets by default. Increase
`--max-targets` explicitly for larger selections. Every mutation requires
confirmation or `--yes`, including one target. Results are ordered by VMID;
one failure does not prevent other guests from completing. Context cancellation
prevents pending operations from being submitted.

Operation statuses are `submitted`, `succeeded`, `failed`, `unknown`, and
`cancel_requested`. UPIDs survive failed waits. `--wait-timeout` defaults to
30 minutes for backup/restore/clone/migrate, 15 minutes for template download,
10 minutes for creation, and five minutes otherwise; override it as needed. Ctrl+C or a
wait timeout cancels only local waiting. Use `task get/log/wait` to recover
tracking. Cancellation checks that the task is running and requests termination;
it does not claim the task already stopped. Non-idempotent requests are never
automatically resubmitted.

Results use stdout, progress and diagnostics use stderr. Use JSON/YAML for
scripts; table layout can change. Exit codes: 0 success, 1 confirmed operation
or strict-health failure, 2 argument/configuration error, 3 API access failure,
4 missing/ambiguous/conflicting target, 5 unknown final outcome.

## Health and node maintenance

```bash
pve check --node pve1 --cpu-warn 85 --memory-warn 90 --strict
pve check --backup-tag backup --backup-max-age 48h
pve status -o json
pve node reboot pve1
pve node shutdown pve1 --yes
```

`doctor` checks CLI setup; `check` checks HomeLab health; `status` aggregates
observed data. Storage retains warn/fail thresholds. Backup checks include a
structured latest timestamp. Inaccessible checks become `unknown`; strict mode
fails on warnings and unknowns. Partial status reports expose `complete=false`
and `component_errors`; their counts describe observed resources only. Tables
mark them `PARTIAL`, so missing queries must not be read as zero resources.

Node reboot/shutdown preflight node state and running VM/LXC counts, show API
and guest impact, and require the exact node name or `--yes`. They never stop
guests automatically and intentionally do not offer `--wait`: API loss cannot
prove a node reboot succeeded. Routine validation uses mock APIs.

## Shell completion

```bash
source <(pve completion bash)
# Zsh: save `pve completion zsh` as _pve in a directory on fpath, then compinit.
# Fish: save `pve completion fish` under ~/.config/fish/completions/pve.fish.
```

Completion covers commands, flags, common enumerations and read-only dynamic
node/storage/VMID candidates. A short Context timeout and silent static fallback
keep unavailable clusters from blocking the shell. Completion never modifies
PVE resources or prints tokens.

<!-- GENERATED_REFERENCE -->

## Global flags

Place global flags before the resource command. `--help` is available on every command.

| Flag | Description and default |
|---|---|
| `--config` | --config value config file path (default: "~/.config/pve/config.yaml") |
| `--profile` | --profile value profile name |
| `--api-timeout` | --api-timeout value PVE API request timeout (default: 0s) |
| `--insecure` | --insecure skip TLS certificate verification (default: false) |
| `--verbose` | --verbose enable verbose logging (default: false) |

## Command reference

Generated from the registered command tree; flags may follow positional IDs.

### `pve status`

Show HomeLab status overview across nodes, guests, storage, and backups

```text
pve status
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve check`

Check HomeLab health (nodes, storage usage, optional backup coverage)

```text
pve check
```

| Flag | Description and default |
|---|---|
| `--cpu-warn` | --cpu-warn value CPU warn percent (default: 85) |
| `--memory-warn` | --memory-warn value memory warn percent (default: 90) |
| `--node` | --node value limit checks to one PVE node |
| `--storage-warn` | --storage-warn value storage usage warn threshold in percent (default: 85) |
| `--storage-fail` | --storage-fail value storage usage fail threshold in percent (default: 95) |
| `--backup-tag` | --backup-tag value check backup coverage for guests with this tag |
| `--backup-max-age` | --backup-max-age value maximum accepted backup age for tagged guests, for example 36h (default: 0s) |
| `--strict` | --strict treat warnings as failures for the exit code (default: false) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve guest ls`

List VM/QEMU and LXC guests

```text
pve guest ls
```

| Flag | Description and default |
|---|---|
| `--name` | --name value name substring |
| `--sort` | --sort value vmid,name,node,status,memory,cpu (default: "vmid") |
| `--desc` | --desc (default: false) |
| `--node` | --node value PVE node name |
| `--type` | --type value guest type: all,vm,lxc (default: "all") |
| `--status` | --status value guest status filter, for example running or stopped |
| `--tag` | --tag value [ --tag value ] filter guests by tag, repeatable |
| `--tag-match` | --tag-match value tag match mode: all,any (default: "all") |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve guest get`

Show VM/QEMU or LXC guest details

```text
pve guest get VMID
```

| Flag | Description and default |
|---|---|
| `--name` | --name value exact name, including numeric names |
| `--node` | --node value PVE node name |
| `--type` | --type value guest type: auto,vm,lxc (default: "auto") |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve guest start`

Start matching guests in bulk

```text
pve guest start
```

| Flag | Description and default |
|---|---|
| `--node` | --node value limit to one PVE node |
| `--type` | --type value guest type: all,vm,lxc (default: "all") |
| `--status` | --status value guest status filter, for example running or stopped |
| `--tag` | --tag value [ --tag value ] filter guests by tag, repeatable |
| `--tag-match` | --tag-match value tag match mode: all,any (default: "all") |
| `--max-targets` | --max-targets value maximum selected guests (default: 20) |
| `--jobs` | --jobs value number of guests to operate on concurrently (default: 2) |
| `--dry-run` | --dry-run show the guests that would be affected and exit (default: false) |
| `--yes` | --yes skip local bulk confirmation (default: false) |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve guest shutdown`

Shutdown matching guests in bulk

```text
pve guest shutdown
```

| Flag | Description and default |
|---|---|
| `--node` | --node value limit to one PVE node |
| `--type` | --type value guest type: all,vm,lxc (default: "all") |
| `--status` | --status value guest status filter, for example running or stopped |
| `--tag` | --tag value [ --tag value ] filter guests by tag, repeatable |
| `--tag-match` | --tag-match value tag match mode: all,any (default: "all") |
| `--max-targets` | --max-targets value maximum selected guests (default: 20) |
| `--jobs` | --jobs value number of guests to operate on concurrently (default: 2) |
| `--dry-run` | --dry-run show the guests that would be affected and exit (default: false) |
| `--yes` | --yes skip local bulk confirmation (default: false) |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve guest reboot`

Reboot matching guests in bulk

```text
pve guest reboot
```

| Flag | Description and default |
|---|---|
| `--node` | --node value limit to one PVE node |
| `--type` | --type value guest type: all,vm,lxc (default: "all") |
| `--status` | --status value guest status filter, for example running or stopped |
| `--tag` | --tag value [ --tag value ] filter guests by tag, repeatable |
| `--tag-match` | --tag-match value tag match mode: all,any (default: "all") |
| `--max-targets` | --max-targets value maximum selected guests (default: 20) |
| `--jobs` | --jobs value number of guests to operate on concurrently (default: 2) |
| `--dry-run` | --dry-run show the guests that would be affected and exit (default: false) |
| `--yes` | --yes skip local bulk confirmation (default: false) |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve guest stop`

Stop matching guests in bulk

```text
pve guest stop
```

| Flag | Description and default |
|---|---|
| `--node` | --node value limit to one PVE node |
| `--type` | --type value guest type: all,vm,lxc (default: "all") |
| `--status` | --status value guest status filter, for example running or stopped |
| `--tag` | --tag value [ --tag value ] filter guests by tag, repeatable |
| `--tag-match` | --tag-match value tag match mode: all,any (default: "all") |
| `--max-targets` | --max-targets value maximum selected guests (default: 20) |
| `--jobs` | --jobs value number of guests to operate on concurrently (default: 2) |
| `--dry-run` | --dry-run show the guests that would be affected and exit (default: false) |
| `--yes` | --yes skip local bulk confirmation (default: false) |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve vm ls`

List virtual machines

```text
pve vm ls
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--name` | --name value case-insensitive name substring |
| `--status` | --status value guest status |
| `--sort` | --sort value vmid,name,node,status,memory,cpu (default: "vmid") |
| `--desc` | --desc sort descending; missing metrics remain last (default: false) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve vm get`

Show guest details

```text
pve vm get VMID
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm start`

Start a guest

```text
pve vm start VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm shutdown`

Shutdown a guest gracefully

```text
pve vm shutdown VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm reboot`

Reboot a guest

```text
pve vm reboot VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm stop`

Stop a guest immediately

```text
pve vm stop VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm agent ping`

Check that the guest agent answers

```text
pve vm agent ping VMID
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm agent network`

Show network interfaces reported by the guest agent

```text
pve vm agent network VMID
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm agent exec`

Run executable+argv inside the guest via the agent

```text
pve vm agent exec VMID -- COMMAND [ARG...]
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--input` | --input value stdin data passed to the command |
| `--timeout` | --timeout value how long to wait for the command to exit (default: 0s) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve vm create`

Create a stopped VM with optional installer ISO

```text
pve vm create VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--name` | --name value |
| `--storage` | --storage value **Required.** |
| `--disk-size` | --disk-size value (default: "32G") |
| `--memory` | --memory value (default: 1024) |
| `--cores` | --cores value (default: 1) |
| `--bridge` | --bridge value (default: "vmbr0") |
| `--iso` | --iso value |
| `--onboot` | --onboot (default: false) |

### `pve vm disk ls`

List attached and unused disks

```text
pve vm disk ls VMID
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm disk add`

Allocate and attach a new disk

```text
pve vm disk add VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--slot` | --slot value **Required.** |
| `--storage` | --storage value **Required.** |
| `--size` | --size value **Required.** |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm disk remove`

Detach a disk while retaining its volume

```text
pve vm disk remove VMID SLOT
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--yes` | --yes skip local confirmation (default: false) |

### `pve vm disk delete`

Destroy an explicitly unused disk volume

```text
pve vm disk delete VMID SLOT
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--yes` | --yes skip local confirmation (default: false) |

### `pve vm config get`

Read native PVE configuration (credentials redacted)

```text
pve vm config get VMID
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm config set`

Set native PVE configuration

```text
pve vm config set VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--set` | --set value [ --set value ] repeatable key=value |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm cloud-init get`

Show cloud-init settings

```text
pve vm cloud-init get VMID
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm cloud-init set`

Update cloud-init settings

```text
pve vm cloud-init set VMID
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--user` | --user value cloud-init user (ciuser) |
| `--password-env` | --password-env value environment variable holding the cloud-init password; never pass passwords as flags |
| `--ssh-key-file` | --ssh-key-file value file with public SSH keys (sshkeys) |
| `--nameserver` | --nameserver value DNS server (nameserver) |
| `--searchdomain` | --searchdomain value DNS search domain (searchdomain) |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--ipconfig0` | --ipconfig0 value ipconfig0 value, for example ip=dhcp |
| `--ipconfig1` | --ipconfig1 value ipconfig1 value |
| `--ipconfig2` | --ipconfig2 value ipconfig2 value |
| `--ipconfig3` | --ipconfig3 value ipconfig3 value |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm cloud-init regenerate`

Regenerate the cloud-init image so the next boot picks up pending changes

```text
pve vm cloud-init regenerate VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm resize`

Resize a guest disk

```text
pve vm resize VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--disk` | --disk value disk identifier **Required.** |
| `--size` | --size value new size or increment, for example +20G **Required.** |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm migrate`

Migrate a guest to another node

```text
pve vm migrate VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value source PVE node name |
| `--target` | --target value target PVE node name **Required.** |
| `--online` | --online request online migration (default: false) |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm clone`

Clone a guest

```text
pve vm clone SOURCE_VMID
```

| Flag | Description and default |
|---|---|
| `--node` | --node value source PVE node name |
| `--newid` | --newid value new VMID/CTID; omitted means automatic NextID (default: 0) |
| `--target` | --target value target PVE node name **Required.** |
| `--storage` | --storage value target storage |
| `--full` | --full perform a full clone (default: false) |
| `--pool` | --pool value target resource pool |
| `--snapname` | --snapname value snapshot name to clone from |
| `--description` | --description value description for the cloned guest |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--name` | --name value name for the cloned VM **Required.** |
| `--format` | --format value target disk format |

### `pve vm snapshot ls`

List snapshots

```text
pve vm snapshot ls VMID
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm snapshot create`

Create a snapshot

```text
pve vm snapshot create VMID SNAPNAME
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value table,json,yaml |
| `--node` | --node value PVE node name |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |

### `pve vm snapshot delete`

Delete a snapshot

```text
pve vm snapshot delete VMID SNAPNAME
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value table,json,yaml |
| `--node` | --node value PVE node name |
| `--dry-run` | --dry-run preview with zero mutation requests (default: false) |
| `--yes` | --yes skip local snapshot delete confirmation (default: false) |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |

### `pve vm snapshot rollback`

Rollback to a snapshot

```text
pve vm snapshot rollback VMID SNAPNAME
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value table,json,yaml |
| `--node` | --node value PVE node name |
| `--dry-run` | --dry-run preview with zero mutation requests (default: false) |
| `--yes` | --yes skip local rollback confirmation (default: false) |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |

### `pve vm backup`

Create a backup for a guest

```text
pve vm backup VMID
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--storage` | --storage value backup storage name **Required.** |
| `--mode` | --mode value backup mode: snapshot,suspend,stop (default: "snapshot") |
| `--compress` | --compress value compression: zstd,lzo,gzip,none (default: "zstd") |
| `--notes-template` | --notes-template value backup notes template |
| `--bwlimit` | --bwlimit value bandwidth limit in KiB/s (default: 0) |
| `--protected` | --protected protect the backup from pruning (default: false) |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--name` | --name value exact guest name (including numeric names) |

### `pve vm restore`

Restore a backup archive into a new, non-existing guest

```text
pve vm restore ARCHIVE
```

| Flag | Description and default |
|---|---|
| `--node` | --node value target PVE node name **Required.** |
| `--vmid` | --vmid value new VMID/CTID; it must not exist yet (default: 0) **Required.** |
| `--storage` | --storage value target storage for guest disks |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve vm delete`

Delete a guest

```text
pve vm delete VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--yes` | --yes skip local delete confirmation (default: false) |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--name` | --name value exact guest name (including numeric names) |

### `pve lxc ls`

List containers

```text
pve lxc ls
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--name` | --name value case-insensitive name substring |
| `--status` | --status value guest status |
| `--sort` | --sort value vmid,name,node,status,memory,cpu (default: "vmid") |
| `--desc` | --desc sort descending; missing metrics remain last (default: false) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve lxc get`

Show guest details

```text
pve lxc get VMID
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--name` | --name value exact guest name (including numeric names) |

### `pve lxc start`

Start a guest

```text
pve lxc start VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--name` | --name value exact guest name (including numeric names) |

### `pve lxc shutdown`

Shutdown a guest gracefully

```text
pve lxc shutdown VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--name` | --name value exact guest name (including numeric names) |

### `pve lxc reboot`

Reboot a guest

```text
pve lxc reboot VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--name` | --name value exact guest name (including numeric names) |

### `pve lxc stop`

Stop a guest immediately

```text
pve lxc stop VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--name` | --name value exact guest name (including numeric names) |

### `pve lxc template download`

Download a system template

```text
pve lxc template download NAME
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--storage` | --storage value **Required.** |

### `pve lxc template ls`

List available or downloaded system templates

```text
pve lxc template ls
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node **Required.** |
| `--storage` | --storage value storage for downloaded templates |
| `--available` | --available query downloadable index (default: false) |
| `--section` | --section value (default: "system") |
| `--name` | --name value name substring |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve lxc create`

Create a stopped system container

```text
pve lxc create CTID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--template` | --template value **Required.** |
| `--rootfs` | --rootfs value **Required.** |
| `--hostname` | --hostname value |
| `--memory` | --memory value (default: 1024) |
| `--cores` | --cores value (default: 1) |
| `--swap` | --swap value (default: 512) |
| `--net0` | --net0 value |
| `--unprivileged` | --unprivileged (default: true) |
| `--ssh-key-file` | --ssh-key-file value |
| `--password-env` | --password-env value |

### `pve lxc config get`

Read native PVE configuration (credentials redacted)

```text
pve lxc config get VMID
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--name` | --name value exact guest name (including numeric names) |

### `pve lxc config set`

Set native PVE configuration

```text
pve lxc config set VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--set` | --set value [ --set value ] repeatable key=value |
| `--name` | --name value exact guest name (including numeric names) |

### `pve lxc resize`

Resize a guest disk

```text
pve lxc resize VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--disk` | --disk value disk identifier **Required.** |
| `--size` | --size value new size or increment, for example +20G **Required.** |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--name` | --name value exact guest name (including numeric names) |

### `pve lxc migrate`

Migrate a guest to another node

```text
pve lxc migrate VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value source PVE node name |
| `--target` | --target value target PVE node name **Required.** |
| `--online` | --online request online migration (default: false) |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--name` | --name value exact guest name (including numeric names) |

### `pve lxc clone`

Clone a guest

```text
pve lxc clone SOURCE_VMID
```

| Flag | Description and default |
|---|---|
| `--node` | --node value source PVE node name |
| `--newid` | --newid value new VMID/CTID; omitted means automatic NextID (default: 0) |
| `--target` | --target value target PVE node name **Required.** |
| `--storage` | --storage value target storage |
| `--full` | --full perform a full clone (default: false) |
| `--pool` | --pool value target resource pool |
| `--snapname` | --snapname value snapshot name to clone from |
| `--description` | --description value description for the cloned guest |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--hostname` | --hostname value hostname for the cloned container **Required.** |

### `pve lxc snapshot ls`

List snapshots

```text
pve lxc snapshot ls VMID
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--name` | --name value exact guest name (including numeric names) |

### `pve lxc snapshot create`

Create a snapshot

```text
pve lxc snapshot create VMID SNAPNAME
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value table,json,yaml |
| `--node` | --node value PVE node name |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |

### `pve lxc snapshot delete`

Delete a snapshot

```text
pve lxc snapshot delete VMID SNAPNAME
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value table,json,yaml |
| `--node` | --node value PVE node name |
| `--dry-run` | --dry-run preview with zero mutation requests (default: false) |
| `--yes` | --yes skip local snapshot delete confirmation (default: false) |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |

### `pve lxc snapshot rollback`

Rollback to a snapshot

```text
pve lxc snapshot rollback VMID SNAPNAME
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value table,json,yaml |
| `--node` | --node value PVE node name |
| `--dry-run` | --dry-run preview with zero mutation requests (default: false) |
| `--yes` | --yes skip local rollback confirmation (default: false) |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |

### `pve lxc backup`

Create a backup for a guest

```text
pve lxc backup VMID
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--storage` | --storage value backup storage name **Required.** |
| `--mode` | --mode value backup mode: snapshot,suspend,stop (default: "snapshot") |
| `--compress` | --compress value compression: zstd,lzo,gzip,none (default: "zstd") |
| `--notes-template` | --notes-template value backup notes template |
| `--bwlimit` | --bwlimit value bandwidth limit in KiB/s (default: 0) |
| `--protected` | --protected protect the backup from pruning (default: false) |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--name` | --name value exact guest name (including numeric names) |

### `pve lxc restore`

Restore a backup archive into a new, non-existing guest

```text
pve lxc restore ARCHIVE
```

| Flag | Description and default |
|---|---|
| `--node` | --node value target PVE node name **Required.** |
| `--vmid` | --vmid value new VMID/CTID; it must not exist yet (default: 0) **Required.** |
| `--storage` | --storage value target storage for guest disks |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve lxc delete`

Delete a guest

```text
pve lxc delete VMID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--node` | --node value PVE node name |
| `--yes` | --yes skip local delete confirmation (default: false) |
| `--wait` | --wait wait for async task completion (default: false) |
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--name` | --name value exact guest name (including numeric names) |

### `pve node reboot`

reboot a node after explicit confirmation

```text
pve node reboot NODE
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--yes` | --yes skip local node-name confirmation (default: false) |

### `pve node shutdown`

shutdown a node after explicit confirmation

```text
pve node shutdown NODE
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--yes` | --yes skip local node-name confirmation (default: false) |

### `pve node ls`

List nodes

```text
pve node ls
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve node get`

Show node details

```text
pve node get NODE
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve task ls`

List recent tasks across nodes

```text
pve task ls
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--type` | --type value filter by task type, for example vzdump |
| `--status` | --status value filter by task status: running,ok,error |
| `--limit` | --limit value show only the N most recent tasks (default: 0) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve task get`

Show task status

```text
pve task get UPID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve task log`

Show task log output

```text
pve task log UPID
```

| Flag | Description and default |
|---|---|
| `--tail` | --tail value show only the last N log lines (default: 0) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve task wait`

Wait for a task to complete

```text
pve task wait UPID
```

| Flag | Description and default |
|---|---|
| `--wait-timeout` | --wait-timeout value task wait timeout (default: 0s) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve task cancel`

Request cancellation of a running task

```text
pve task cancel UPID
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |
| `--yes` | --yes skip local confirmation (default: false) |

### `pve storage ls`

List storage status

```text
pve storage ls
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--content` | --content value filter by content type, for example backup or iso |
| `--type` | --type value filter by storage type, for example dir or lvmthin |
| `--active` | --active show only active storages (default: false) |
| `--enabled` | --enabled show only enabled storages (default: false) |
| `--sort` | --sort value name,node,used-percent,total |
| `--desc` | --desc (default: false) |
| `--min-used-percent` | --min-used-percent value minimum known usage percent (default: 0) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve storage usage`

Show storage usage across nodes

```text
pve storage usage
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--content` | --content value filter by content type, for example backup or iso |
| `--type` | --type value filter by storage type, for example dir or lvmthin |
| `--active` | --active show only active storages (default: false) |
| `--enabled` | --enabled show only enabled storages (default: false) |
| `--sort` | --sort value name,node,used-percent,total |
| `--desc` | --desc (default: false) |
| `--min-used-percent` | --min-used-percent value minimum known usage percent (default: 0) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve storage get`

Show storage status on a node

```text
pve storage get STORAGE
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name **Required.** |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve storage content ls`

List content on a storage

```text
pve storage content ls
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name **Required.** |
| `--storage` | --storage value storage name **Required.** |
| `--content` | --content value filter by content type, for example backup or iso |
| `--vmid` | --vmid value filter by VMID/CTID (default: 0) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve backup ls`

List backup files on a storage

```text
pve backup ls
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name **Required.** |
| `--storage` | --storage value backup storage name **Required.** |
| `--vmid` | --vmid value filter by VMID/CTID (default: 0) |
| `--type` | --type value backup type: all,vm,lxc (default: "all") |
| `--latest` | --latest show only latest backup per guest (default: false) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve network ls`

List network interfaces

```text
pve network ls
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name |
| `--type` | --type value filter by interface type, for example bridge, bond, or vlan |
| `--active` | --active show only active interfaces (default: false) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve network get`

Show network interface details

```text
pve network get IFACE
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name **Required.** |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve firewall status`

Show whether the firewall is enabled for a scope

```text
pve firewall status
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name; required for --scope node, located automatically for vm and lxc |
| `--scope` | --scope value firewall scope: node,vm,lxc (default: "node") |
| `--vmid` | --vmid value VMID/CTID; required when --scope is vm or lxc (default: 0) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve firewall ls`

List firewall rules for a scope

```text
pve firewall ls
```

| Flag | Description and default |
|---|---|
| `--node` | --node value PVE node name; required for --scope node, located automatically for vm and lxc |
| `--scope` | --scope value firewall scope: node,vm,lxc (default: "node") |
| `--vmid` | --vmid value VMID/CTID; required when --scope is vm or lxc (default: 0) |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve config ls`

List profile names and endpoints

```text
pve config ls
```

### `pve config show`

Show the current or named profile, or all profiles, as tables

```text
pve config show [NAME]
```

| Flag | Description and default |
|---|---|
| `--all` | --all show all profiles (default: false) |

### `pve config add`

Interactively add a profile

```text
pve config add
```

### `pve config set`

Create or replace a profile

```text
pve config set NAME
```

| Flag | Description and default |
|---|---|
| `--endpoint` | --endpoint value PVE API endpoint, for example https://pve.lan:8006/api2/json |
| `--token-id` | --token-id value PVE API token id, for example automation@pve!pve |
| `--token-secret-file` | --token-secret-file value private credential file |
| `--token-secret-stdin` | --token-secret-stdin read token from stdin and store in a private file (default: false) |
| `--token-secret-env` | --token-secret-env value environment variable containing the PVE API token secret |
| `--insecure` | --insecure skip TLS certificate verification for this profile (default: false) |
| `--timeout` | --timeout value PVE API request timeout for this profile (default: "30s") |
| `--default-output` | --default-output value default output format: table,json,yaml (default: "table") |

### `pve config use`

Select the current profile

```text
pve config use NAME
```

### `pve doctor`

Diagnose pve config and Proxmox API connectivity

```text
pve doctor
```

| Flag | Description and default |
|---|---|
| `--offline` | --offline check local config only without connecting to Proxmox VE (default: false) |
| `--node` | --node value verify that a node exists during online checks |
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve version`

Show pve build and runtime version information

```text
pve version
```

| Flag | Description and default |
|---|---|
| `--output`, `-o` | --output value, -o value output format: table,json,yaml |

### `pve completion bash`

Print bash completions

```text
pve completion bash
```

### `pve completion zsh`

Print zsh completions

```text
pve completion zsh
```

### `pve completion fish`

Print fish completions

```text
pve completion fish
```
