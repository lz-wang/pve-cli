# Compatibility Policy

`pvectl` v1.x is intended to be stable for personal HomeLab scripts and
automation. This policy applies to documented behavior.

## Stable Within v1.x

- Command names and documented subcommand structure.
- Positional argument order and meaning.
- Documented flags and their accepted value shapes.
- JSON and YAML output field names and field types.
- Command results on stdout.
- Task IDs and wait progress on stderr.
- Local confirmation behavior for dangerous operations.
- Doctor diagnostics as structured rows, including failure rows on stdout.

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

Table output is for humans and may be adjusted for readability in v1.x. Scripts
should use `-o json` or `-o yaml`.

## In Scope

One-off VM/LXC backup restore is supported. Restoring a vzdump archive into a
new, non-existing VMID is a supported disaster-recovery workflow. Overwriting an
existing VMID during restore is a non-goal; delete the guest first, then
restore.

## Non-goals

- Scheduled backup job management.
- Backup prune policy management.
- PBS datastore administration.
- PBS verification/prune administration.
- HA/Ceph/SDN full management.
- Arbitrary Proxmox REST API passthrough.

See the README for the full project non-goals list.
