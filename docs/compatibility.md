# Compatibility Policy

Development on `main` is the v2 line. The next release will be **v2.0.0**, not
v1.2.0: the changes collected under v2.0.0 remove and rename documented v1
commands, flags, and defaults, which this policy defines as breaking. v2.0.0
supersedes the v1.x stability promise instead of restoring compatibility
stubs for it.

Within v2.x, this tool is intended to be stable for personal HomeLab scripts
and automation. This policy applies to documented behavior.

## v1 to v2 Migration

v2.0.0 intentionally breaks the documented v1 CLI surface. The main changes:

- The executable is `pve` and the Homebrew formula is `lz-wang/tap/pve`; no
  `pvectl` executable alias is provided.
- The default config path is `~/.config/pve/config.yaml`; the old directory is
  not searched or migrated automatically. Existing config files can be moved
  there or selected with `--config`. Token-secret environment variable names
  remain user-defined.
- Global `--timeout` is renamed to `--api-timeout` so it no longer collides
  with the guest-command `--timeout` of `vm agent exec`.
- Global `--output`, `--wait`, and `--wait-timeout` are removed; they are
  per-command flags on the commands that use them.
- `config update` is replaced by `config set` and `config use`, and the
  historical `view`, `init`, `set-profile`, `use-profile`, `current-profile`,
  `set-context`, `use-context`, and `current-context` subcommands are removed.
- `firewall --type` is renamed to `--scope`, `backup ls --kind` to `--type`,
  and `vm cloud-init update` to `vm cloud-init regenerate`.
- `vm/lxc backup --protected` is a boolean flag instead of a literal `0` or
  `1` value.

See `CHANGELOG.md` for the complete list. There are no hidden aliases or
compatibility stubs for removed v1 names; update scripts to the new surface.

## Stable Within v2.x

- Command names and documented subcommand structure.
- Positional argument order and meaning.
- Documented flags and their accepted value shapes.
- JSON and YAML output field names and field types.
- Command results on stdout.
- Task IDs, wait progress, and bulk-operation progress on stderr.
- Local confirmation behavior for dangerous operations, including snapshot
  delete and multi-guest bulk operations.
- Bulk operations write one result row per guest to stdout and exit non-zero
  only when at least one guest failed.
- Doctor and check diagnostics as structured rows, including failure rows on
  stdout.

## Allowed Non-breaking Changes

- Add new commands.
- Add new optional flags.
- Add new JSON/YAML fields.
- Improve table output formatting.
- Improve error messages without changing exit semantics.
- Add more doctor diagnostic rows.

## Breaking Changes

These require a new major version:

- Remove or rename documented commands.
- Remove or rename documented flags.
- Change positional argument meaning.
- Remove or rename JSON/YAML fields.
- Change JSON/YAML field types.
- Move command result output from stdout to stderr.
- Move task IDs or wait progress from stderr to stdout.
- Skip dangerous-operation confirmation by default.

## Table Output

Table output is for humans and may be adjusted for readability within a major
version. Scripts should use `-o json` or `-o yaml`.

## In Scope

One-off VM/LXC backup restore is supported. Restoring a vzdump archive into a
new, non-existing VMID is a supported disaster-recovery workflow. Overwriting
an existing VMID during restore is a non-goal; delete the guest first, then
restore.

## Non-goals

- Scheduled backup job management.
- Backup prune policy management.
- PBS datastore administration.
- PBS verification/prune administration.
- HA/Ceph/SDN full management.
- Arbitrary Proxmox REST API passthrough.

See the README for the full project non-goals list.
