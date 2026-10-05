# Roadmap

## Current stable scope

- config initialization and diagnostics
- HomeLab status aggregation and health checks (`status`, `check`)
- VM/QEMU daily operations
- LXC daily operations
- read-only guest aggregate views combining VM/QEMU and LXC
- guest tag filtering and bulk lifecycle operations with dry-run
- task inspection (`task ls/get/log/wait`)
- clone/config/resize/migrate/snapshot (including snapshot delete) for
  HomeLab maintenance
- read-only backup listing
- one-off VM/LXC backup trigger
- one-off VM/LXC backup restore into a new, non-existing VMID
- VM QEMU guest agent: ping, network, exec
- VM cloud-init: get, set, regenerate (PVE-native)
- read-only network inventory
- read-only firewall inventory (node/VM/LXC)
- node detail inspection
- storage read-only inventory and usage view
- stable JSON/YAML output contracts
- v2.x compatibility policy

## Candidate future features

- shell-friendly query helpers
- optional deeper doctor/check checks

## Non-goals

See README.

## Non-goals

See README.
