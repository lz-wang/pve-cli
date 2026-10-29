# pve v3 features

A resource-oriented, single-binary Proxmox VE CLI for a personal HomeLab.
See [usage](docs/usage.md) for every command, flag, default and argument, and
[output schema](docs/output-schema.md) for the JSON/YAML contract.

## CLI and profiles

- `pve` executable; `github.com/lz-wang/pve-cli/v3` Go module; urfave/cli/v2.
- Dashboard, Guests, Infrastructure and Local help groups.
- Per-command table/JSON/YAML results on stdout; progress and UPIDs on stderr.
- Global config/profile, API timeout, TLS and verbose controls; local task waits.
- `config ls/show/add/set/use`, guided setup, hidden token input, current-profile
  selection, separate credential files and environment references. Config show
  prints references and an absolute path without resolving secret values.
- Strict YAML schema; exactly one token source; Unix private credential files
  owned by the user; atomic writes and private managed directories.
- Bash, Zsh and Fish static command/flag/enum completion and read-only dynamic
  node, storage and VMID candidates, with bounded timeout and silent fallback.

## Guest inspection and lifecycle

- `vm/lxc ls/get` and aggregate `guest ls/get` with exact ID/name resolution,
  explicit numeric names, ambiguity reporting and complete inventory checks.
- Name/status/tag filters, all/any tag matching, and stable sorting by
  VMID/name/node/status/memory/CPU; missing metrics remain last.
- Single-guest start, shutdown, reboot and stop; tracked async operation results.
- Mixed VM/LXC bulk start/shutdown/reboot/stop with selection guards, dry-run,
  confirmation, 1–8 workers, default 20-target limit and per-guest outcomes.
- Context cancellation prevents pending submissions; partial failures remain
  independently visible and VMID ordering stays stable.

## Deployment and maintenance

- LXC downloadable system-template index and local `vztmpl` inventory, section
  and name filtering, duplicate-safe downloads with post-wait file verification.
- LXC creation checks ID availability, template, rootdir storage and networking;
  defaults to unprivileged, stopped containers; supports SSH public keys and
  environment-sourced passwords.
- VM creation configures CPU, memory, SCSI disk, bridge, optional ISO boot and
  onboot. It stays stopped and does not install an OS.
- VM/LXC native config get/set preserves native keys/types under `config`,
  redacts credential values and rejects duplicate update keys.
- VM disks list system/attached/installer/unused roles; add checks allocation
  slots and image storage; remove retains a volume in an unused slot; delete
  only destroys explicitly unused volumes with exact confirmation and storage
  verification. Existing resize remains available.
- VM/LXC clone, resize and migration; snapshots list/create/rollback/delete.
- Snapshot rollback/delete preflight existence and locks, support read-only
  dry-run, and require confirmation. VM rollback requires a stopped VM.
- VM cloud-init get/set/regenerate using native PVE configuration; clone a
  prepared template for cloud-init deployment.
- VM QEMU agent ping, network inventory and executable/argv-based exec.
- Backup inventory with filters/latest selection; one-off VM/LXC backups and
  restore into a new unoccupied VMID. No automatic destructive overwrite.

## Infrastructure and health

- Node list/details and guarded reboot/shutdown with running-guest counts,
  exact node confirmation and impact text; no automatic guest stop or wait.
- Storage inventory/details/content/usage with content/type/state filters,
  name/node/used-percent/total sorting and usage thresholds. Unknown capacity
  sorts last; shared rows remain per-node without duplicate capacity sums.
- Read-only node network inventory and node/VM/LXC firewall status/rule views.
- Task list/get/log/wait, status/type filters, bounded log tail and cancellation
  requests checked against running state. Cancellation returns cancel_requested.
- Uniform submitted/succeeded/failed/unknown operation states, recoverable UPIDs,
  Context-bound waits and no automatic resubmission of non-idempotent requests.
- Doctor validates local setup, credentials and API connectivity without secrets.
- Health checks cover node state, CPU/memory thresholds, storage warn/fail
  thresholds and tagged backup freshness with a structured latest timestamp.
  Inaccessible checks are unknown; strict mode fails on warning/unknown results.
- Status aggregates nodes/guests/storage/backups with complete/component_errors,
  observed counts and a PARTIAL table indicator for incomplete reports.

## Contracts and delivery

- Exit codes 0 success, 1 operation/health failure, 2 input/configuration,
  3 API access, 4 resolution/conflict, 5 unknown outcome.
- `--yes` skips local confirmation only; nonterminal dangerous operations require
  it. Dry-run performs no mutation; waiting never cancels the remote task.
- Typed and secret-safe HTTP failures; no raw credential response bodies.
- Mock service, command, HTTP contract, JSON/YAML golden and race tests.
- Generated command/schema references checked against source definitions.
- Linux/macOS/Windows amd64/arm64 release builds, checksums, GitHub Release notes
  from Changelog and Homebrew formula publication.

The scope stays small: no daemon, Web UI, multi-user control plane, arbitrary
REST passthrough, scheduled backup policy, or full HA/Ceph/SDN management.
