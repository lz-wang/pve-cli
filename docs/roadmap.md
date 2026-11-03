# Roadmap

## Implemented in v3

- Reliability pass (P0): structured sanitized `APIError` with HTTP status,
  field-level validation errors and operation context; post-write state
  verification for `config set` (`verified`/`api_error` semantics, no
  retries on uncertain outcomes); requested vs effective backup mode
  reporting sourced from task logs.
- Configuration and LXC execution (P1): attribute-wise LXC network device
  updates with bridge existence checks and digest-based conflict detection;
  optional per-profile SSH transport over the system OpenSSH client;
  `lxc exec` running container commands via `pct exec` with remote exit
  codes preserved and SSH connection failures distinguished.
- Cross-profile migration (P2): read-only migration preflight
  (`lxc migrate plan`) across independent PVE profiles, native
  `remote_migrate` execution with credential-transfer notices and target
  token redaction (`lxc migrate remote`), and post-migration validation of
  both sides (`lxc migrate check`); no automatic rollback.
- Backup transfer and restore (P3): cross-profile backup archive streaming
  with checksum verification and limited resume (`backup copy`), preflighted
  restores with bridge mapping (`vm/lxc restore`), and the composed
  backup→transfer→verify→restore→post-check pipeline (`backup
  copy-restore`) with per-stage results and documented resume rules.

- Cluster-aware unique resource resolution and typed failures.
- Exact guest names, filtered/sorted inventory and native configuration.
- Recoverable async operation results, bounded waits and task cancellation.
- Safe mixed bulk lifecycle operations with bounded workers and target limits.
- Private file/environment credentials and hidden/standard-input token setup.
- Bash/Zsh/Fish static and read-only dynamic completion.
- System LXC template inventory/download and unprivileged container creation.
- Empty/ISO VM creation, disk allocation, safe detach and unused-volume deletion.
- CPU/memory/storage/backup health checks and explicit partial status reports.
- Storage sorting/thresholds and snapshot previews/preconditions.
- Guarded node reboot/shutdown with no automatic guest stop.
- Clone/resize/migration, snapshots, backups/restore, QEMU agent, cloud-init,
  read-only network/firewall/storage inspection.
- Generated CLI/schema references, HTTP contracts and cross-platform release builds.

## Future candidates

- Additional read-only completion enums and health diagnostics.
- More isolated real-PVE integration coverage for exceptional API states.
- Further resource-specific validation as PVE API schemas evolve.

OCI application containers, scheduled backup/prune policy management, PBS
administration, full HA/Ceph/SDN management, arbitrary REST passthrough, a server
mode and a multi-user control plane are outside the current scope.
