# Roadmap

## Implemented in v3

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
