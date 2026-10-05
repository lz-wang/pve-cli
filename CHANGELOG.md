# Changelog

## v2.0.0 (Unreleased)

The breaking changes in this section supersede the v1.x stability promise;
see `docs/compatibility.md` for the v1 to v2 migration summary. No v1
compatibility aliases or hidden stubs are provided.

### Added

- Support plaintext `token_secret` in YAML profiles, hidden token input in
  `pve config add`, and `--token-secret` for `pve config set`. A non-empty
  plaintext value takes precedence over `token_secret_env`; connection writes
  require at least one token source.
- Add doctor diagnostics for plaintext tokens without printing their values,
  and redact token values from backend error messages.
- Guide interactive initialization from `pve config ls` and `pve config show`
  when the config file is missing, with hidden token input, optional environment
  references, clean cancellation, nonterminal add/set setup hints, and no
  overwrite of existing files.

### Changed

- Remove the ineffective `-o`/`--output` flag from `vm cloud-init set` and
  `vm cloud-init regenerate`; both write fixed plain-text confirmations instead
  of structured output, so only `vm cloud-init get` keeps the flag.
- Make `--node` optional for `firewall status`/`firewall ls` with vm/lxc
  scopes: when omitted, the guest is located across the cluster like other
  VMID-oriented commands. The node scope still requires `--node`.
- Rename firewall `--type` to `--scope`, `backup ls --kind` to `--type`, and
  `vm cloud-init update` to `vm cloud-init regenerate` so each name keeps one
  stable meaning across commands.
- Turn `vm/lxc backup --protected` into a boolean flag instead of requiring a
  literal `0` or `1` value.
- Group top-level help commands into Dashboard, Guests, Infrastructure, and
  Local categories rendered as real help sections, and tighten usage wording:
  `guest` now reads "Inspect and operate on ...", `node` reads "Inspect PVE
  nodes", and `vm ls`/`lxc ls` list virtual machines/containers explicitly.
- Derive argument normalization from the CLI command tree itself instead of a
  parallel hand-maintained flag list, so new commands and flags reorder
  resource IDs and flags correctly without updating normalizer metadata.
- Rename the global API request timeout flag from `--timeout` to
  `--api-timeout` so it no longer collides with the `--timeout` flag of
  `vm agent exec`, which bounds the guest command instead.
- Remove the global `--output` flag; `-o`/`--output` is now a per-command flag
  on commands with structured output, so a global flag can no longer be
  silently ignored by table-only commands such as `config show`.
- Remove the global `--wait` and `--wait-timeout` flags; async operation
  commands keep their own `--wait` and `--wait-timeout` flags.
- Order help commands by HomeLab role and typical use, with daily inspection
  and lifecycle commands first, local setup tools later, and guest deletion
  and snapshot rollback at the end of their command lists.
- Rename the CLI executable from `pvectl` to `pve`, including help, version
  output, local build/install targets, release assets, archives, and the
  Homebrew formula (`lz-wang/tap/pve`). No `pvectl` executable alias is provided.
- Use `~/.config/pve/config.yaml` as the default config path without automatic
  lookup or migration of the old config directory. Existing config files can
  be moved to the new path or selected explicitly with `--config`.
- Update CLI documentation and examples to use `pve` and `PVE_*` environment
  variable names. Token-secret environment variable names remain user-defined.
- Save config files with permissions `0600` on Unix, including existing files.
- Replace the config subcommands with `ls`, `show`, `add`, `set`, and `use`.
  List names and endpoints in name order; show one selected profile or all
  profiles in detail tables. Add profiles interactively without overwriting
  existing names. Create or replace profiles noninteractively with
  `set NAME`; it never changes the current profile. Switch the current profile
  with `use NAME`. Remove `view`, `init`, `set-profile`, `current-profile`,
  `use-profile`, `set-context`, `use-context`, `current-context`, and the
  short-lived `update` without a compatibility layer.
- Mask stored plaintext `token_secret` in `pve config show` output, retaining
  the first and last three Unicode characters for values longer than six
  characters and using only `*****` for values of six characters or fewer.
  Append the resolved absolute config-file path as a final `Config file:` line.

## v1.1.0 - 2026-10-04

### Added

- Add `pvectl status` for a HomeLab-wide snapshot of nodes, guests, storages,
  and backups, with partial-failure issues instead of aborted reports.
- Add `pvectl check` for current-health checks with storage usage thresholds,
  optional backup coverage via `--backup-tag` and `--backup-max-age`, and
  `--strict` warning handling for cron and agent consumers.
- Add task inspection commands: `pvectl task ls`, `pvectl task get`,
  `pvectl task log`, and `pvectl task wait`.
- Add one-off backup restore with `pvectl vm restore` and `pvectl lxc restore`
  into a new, non-existing VMID, with a fail-closed cluster-wide VMID preflight
  check.
- Add safe snapshot deletion with `pvectl vm snapshot delete` and
  `pvectl lxc snapshot delete`, confirmed locally like other dangerous
  operations.
- Add QEMU guest agent commands: `pvectl vm agent ping`,
  `pvectl vm agent network`, and `pvectl vm agent exec`.
- Add VM cloud-init management: `pvectl vm cloud-init get`, `set`, and
  `update` with normalized config output.
- Add read-only network inventory with `pvectl network ls` and
  `pvectl network get`.
- Add read-only firewall inventory with `pvectl firewall status` and
  `pvectl firewall ls` for node, VM, and LXC scopes.
- Add bulk guest lifecycle operations (`pvectl guest start`, `stop`,
  `shutdown`, `reboot`) with tag/status selection, `--dry-run`, and structured
  per-guest results.
- Add guest tag filtering with `--tag` and `--tag-match any|all`.
- Add `pvectl node get` for detailed node inspection and `pvectl storage
  usage` as a compact storage usage view.

### Changed

- Upgrade go-proxmox to v0.8.1 and route LXC restore through the typed
  container create wrapper.
- Parse Proxmox guest tags with the canonical `;` separator.
- Resolve guest nodes consistently for VMID-scoped commands: agent and
  cloud-init commands accept an omitted `--node` and resolve across nodes.
- Preserve whole SSH public key lines in cloud-init updates.
- Fail bulk mutations closed when the guest selection cannot be fully listed,
  while read commands keep partial-success behavior.
- Report `backup status unavailable` when backup storages cannot be queried,
  and keep coverage indeterminate when only some backup sources fail.
- Deduplicate shared backup storages across nodes and query them on a
  readable (enabled and active) node with fallback on failure.
- Publish per-version archives to WebDAV and update the Homebrew tap formula
  on tag pushes.

### Fixed

- Fix LXC restore to send the Proxmox restore parameters (`ostemplate` plus
  `restore=true`) instead of the rejected `archive` parameter.
- Fix restore preflight to abort when any node's guest inventory cannot be
  verified, and preserve the structured restore result when the task wait
  fails.
- Keep CLI argument parsing in sync with flags that take values.
- Fix cloud-init SSH key handling that previously split keys into words.

### Notes

- Restore never overwrites an existing guest; the target VMID must not exist.
- Bulk mutations abort entirely unless every selected guest could be listed.
- `status` and `check` are read-only and machine-consumable; command results
  go to stdout, task IDs and wait progress go to stderr.
- No server mode, Web UI, RBAC, audit, billing, network or firewall mutation,
  or generic Proxmox REST API passthrough is included.

## v1.0.0 - 2026-06-06

### Added

- Add `pvectl version` with table, JSON, and YAML output.
- Add build metadata for version, commit, date, Go version, OS, and arch.
- Use profiles for user-facing config selection in v1.0.0.
- Add v1.x output schema documentation for script-facing JSON/YAML fields.
- Add v1.x compatibility policy for commands, flags, structured output, and
  stdout/stderr behavior.
- Add command golden tests for key JSON outputs.
- Add output contract tests for public structured output types.
- Add `make install` and `make uninstall` targets.

### Changed

- Treat JSON/YAML output field names and field types as stable within v1.x.
- Keep table output documented as human-facing rather than script-facing.
- Inject commit and build date metadata in local and GitHub Actions builds.
- Use the matching `CHANGELOG.md` version section as GitHub Release notes.

### Notes

- No new Proxmox mutation APIs are added.
- No server mode, Web UI, RBAC, audit, restore, prune, PBS management, storage
  write support, generic filters, or API passthrough is included.

## v0.9 - 2026-06-06

### Added

- Add `pvectl config init` for one-command HomeLab context initialization.
- Add `pvectl doctor` for local config and Proxmox API connectivity
  diagnostics.
- Add doctor output rows for table, JSON, and YAML output.
- Add offline doctor mode with `--offline`.
- Add optional node existence checks with `--node`.

### Notes

- Doctor output never prints token secret values.
- Doctor checks only `/nodes` for online API validation.
- No new Proxmox mutation APIs, server mode, database, RBAC, audit, restore,
  prune, PBS management, or storage write support is included.

## v0.8 - 2026-06-05

### Added

- Add `pvectl storage ls` for read-only node storage inventory.
- Add `pvectl storage get <storage> --node <node>` for detailed storage status.
- Add `pvectl storage content ls` for read-only storage content inventory.
- Add storage output schemas for storage status and generic storage content.
- Add storage filters for content type, storage type, active/enabled status,
  and VMID.

### Notes

- Storage support is intentionally read-only.
- No storage creation, update, deletion, upload, download, pruning, or PBS
  management is included.

## v0.7 - 2026-06-05

### Added

- Add `pvectl backup ls` for listing backup files on a specified node and
  storage.
- Add `pvectl vm backup <vmid>` for one-off VM/QEMU backups.
- Add `pvectl lxc backup <vmid>` for one-off LXC backups.
- Add backup output schemas for backup rows and backup task results.
- Add backup filters for `--vmid`, `--kind`, and `--latest`.

### Notes

- Backup support is intentionally limited to listing backup files and creating
  one-off guest backups.
- No backup deletion, restore, prune, scheduled backup job management, or PBS
  management is included.

## v0.6 - 2026-06-05

### Added

- Add read-only `guest` aggregate commands for inventory across VM/QEMU and LXC
  guests.
- Add `pvectl guest ls` with `--type all|vm|lxc`, `--node`, and `--status`
  filters.
- Add `pvectl guest get <vmid>` with `--type auto|vm|lxc`.
- Add aggregate table output with a `KIND` column so VM and LXC rows can be
  distinguished.
- Add GitHub Actions release automation for pushed tags, including multi-platform
  builds, checksums, Release asset upload, and Pushover notifications.
- Add `make dist` for local multi-platform release builds.

### Changed

- Keep ordinary branch pushes to test and build only; tag pushes publish release
  assets.
- Skip duplicate branch build/notification work when `git push origin main --tags`
  also triggers a tag workflow for the same commit.
- Update README and usage docs with the `guest` aggregate workflow.

### Notes

- `guest` is intentionally read-only. Mutating lifecycle and maintenance
  operations remain under `vm` and `lxc`.

## v0.5.1 - 2026-06-05

### Changed

- Refine project positioning docs around the personal HomeLab CLI scope.
- Add agent guidance for repository structure, documentation boundaries, security
  expectations, and common validation commands.
- Update roadmap documentation.

## v0.5 - 2026-06-05

### Added

- Add `pvectl vm reboot <vmid>` and `pvectl lxc reboot <vmid>`.
- Document reboot as part of the daily VM/QEMU and LXC lifecycle workflow.

### Changed

- Reorganize command code by guest operation area:
  - core lifecycle commands
  - clone and config commands
  - maintenance commands
  - dangerous commands
  - snapshot commands
- Restructure usage documentation so daily commands stay separate from
  maintenance, snapshots, dangerous operations, output formats, and scripting
  notes.
- Keep README focused on install, configuration, daily usage, and non-goals.

## v0.4 - 2026-06-05

### Added

- Add VM/QEMU snapshot listing with `pvectl vm snapshot ls <vmid>`.
- Add VM/QEMU snapshot creation with `pvectl vm snapshot create <vmid> <name>`.
- Add VM/QEMU snapshot rollback with
  `pvectl vm snapshot rollback <vmid> <name>`.
- Add LXC snapshot listing with `pvectl lxc snapshot ls <vmid>`.
- Add LXC snapshot creation with `pvectl lxc snapshot create <vmid> <name>`.
- Add LXC snapshot rollback with `pvectl lxc snapshot rollback <vmid> <name>`.
- Add local confirmation for snapshot rollback unless `--force` is passed.

### Notes

- Snapshot create and rollback are asynchronous Proxmox tasks and support
  `--wait`.

## v0.3 - 2026-06-05

### Added

- Add VM/QEMU migration with `pvectl vm migrate <vmid> --target <node>`.
- Add LXC migration with `pvectl lxc migrate <vmid> --target <node>`.
- Add VM/QEMU disk resize with
  `pvectl vm resize <vmid> --disk <disk> --size <size>`.
- Add LXC disk resize with
  `pvectl lxc resize <vmid> --disk <disk> --size <size>`.
- Add VM/QEMU delete with `pvectl vm delete <vmid>`.
- Add LXC delete with `pvectl lxc delete <vmid>`.
- Add local delete confirmation prompts, with `--force` available to skip the
  local prompt.

### Notes

- Delete, migrate, and resize commands support the existing async task waiting
  flow where applicable.

## v0.2 - 2026-06-05

### Added

- Add VM/QEMU clone with `pvectl vm clone <source-vmid>`.
- Add LXC clone with `pvectl lxc clone <source-vmid>`.
- Add clone options for explicit IDs, generated IDs, target node, storage, full
  clone mode, and guest name/hostname.
- Add VM/QEMU config updates with `pvectl vm config <vmid> --set key=value`.
- Add LXC config updates with `pvectl lxc config <vmid> --set key=value`.
- Return clone results with `new_vmid` for script-friendly JSON/YAML output.

## v0.1 - 2026-06-05

### Added

- Add initial `pvectl` CLI entrypoint and version wiring.
- Add YAML config management with contexts:
  - `pvectl config set-context`
  - `pvectl config use-context`
  - `pvectl config current-context`
  - `pvectl config view`
- Store Proxmox API token secret references through `token_secret_env` instead
  of writing token secret values to disk.
- Add global flags for config path, context, output format, timeout, TLS
  verification, and verbose mode.
- Add table, JSON, and YAML output rendering.
- Add node listing with `pvectl node ls`.
- Add VM/QEMU list, get, start, shutdown, and stop commands.
- Add LXC list, get, start, shutdown, and stop commands.
- Add automatic guest lookup across nodes when `--node` is omitted.
- Add async task handling with `--wait` and `--wait-timeout`; task IDs and wait
  progress go to stderr while command results go to stdout.
