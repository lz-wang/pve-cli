# AGENTS.md

Guidance for agents and automation working in this repository.

## Project Positioning

`pve` is a personal HomeLab Proxmox VE CLI. It wraps the Proxmox VE API
through `go-proxmox` and exposes resource-oriented commands for daily VM/QEMU
and LXC operations.

Keep the tool small and predictable. Do not turn it into:

- a Web UI
- a server mode or HTTP API
- a multi-user control plane
- an RBAC, audit, billing, or policy platform
- a full Proxmox REST API passthrough

## Repository Layout

- `cmd/` contains CLI command definitions, argument parsing, confirmation
  prompts, and command-level tests.
- `internal/pve/` contains the Proxmox backend, guest services, task handling,
  and `go-proxmox` wrappers.
- `internal/config/` contains YAML config loading, saving, profile selection,
  and plaintext/environment token-secret resolution.
- `internal/output/` contains table, JSON, and YAML rendering.
- `docs/usage.md` contains the complete command reference.
- `README.md` should stay short and focused on the main HomeLab path.

## Development Rules

- Preserve existing CLI compatibility unless a task explicitly asks for a
  breaking change.
- Prefer the existing flow: `cmd -> internal/pve service -> go-proxmox`.
- Reuse the current `Backend`, `Guest`, and `Task` abstractions for tests.
- Avoid adding controllers, repositories, use cases, policy engines, servers,
  databases, or other platform-style layers.
- Keep `guest` commands read-only unless a future task explicitly changes the
  scope.
- Keep `storage` commands read-only unless a future task explicitly changes the
  scope.
- Keep dangerous operations explicit and locally confirmed unless the existing
  `--force` behavior already applies.
- Keep command output script-friendly: command results go to stdout; task IDs
  and wait progress go to stderr.
- Doctor commands must not print token secrets.
- Doctor should prefer structured diagnostic rows over early returns so users
  can see multiple failures in one run.

## v1 Compatibility Rules

- Do not rename or remove documented commands or flags in v1.x.
- Do not change documented positional argument order or meaning in v1.x.
- Do not rename, remove, or change the type of JSON/YAML output fields in v1.x.
- Adding new JSON/YAML fields is allowed.
- Table output may be adjusted for readability, but command results must remain
  on stdout.
- Task IDs and wait progress must remain on stderr.
- Dangerous operations must keep local confirmation unless existing `--force`
  behavior applies.

## Documentation Rules

- Keep README examples focused on daily usage: node list, guest list/get,
  start, shutdown, reboot, and stop.
- Put clone, config, resize, migrate, snapshot, delete, output formats, and
  scripting details in `docs/usage.md`.
- Keep `FEATURES.md` and `FEATURES-zhCN.md` synchronized with user-facing tool
  capabilities. Whenever commands, flags, output contracts, safety behavior, or
  supported workflows change, update both files in the same change.
- Keep dangerous commands such as delete and snapshot rollback separate from
  daily examples.
- Before creating or pushing a release tag, update `CHANGELOG.md` with the new
  version entry.
- GitHub Release notes should be based on the matching version section in
  `CHANGELOG.md`.

## Security Rules

- Never commit API token secrets or real credentials.
- Config files support plaintext `token_secret` and `token_secret_env` references.
  A non-empty `token_secret` takes precedence over the environment reference.
- Prefer examples that use placeholder endpoints, token IDs, and environment
  variable names.

## Common Commands

```bash
make fmt
make check
make test
make build
```

For documentation-only changes, `git diff --check` is sufficient unless the
task asks for broader validation.
