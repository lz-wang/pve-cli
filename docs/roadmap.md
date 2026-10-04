# Roadmap

## Current stable scope

- config initialization and diagnostics
- VM/QEMU daily operations
- LXC daily operations
- read-only guest aggregate views combining VM/QEMU and LXC
- clone/config/resize/migrate/snapshot for HomeLab maintenance
- read-only backup listing
- one-off VM/LXC backup trigger
- one-off VM/LXC backup restore into a new, non-existing VMID
- storage read-only inventory
- stable JSON/YAML output contracts
- v1.x compatibility policy

## Candidate future features

- task inspection commands (`task ls/get/log/wait`)
- HomeLab status aggregation
- guest tag filtering and bulk operations
- node detail inspection and HomeLab health checks
- shell-friendly query helpers
- optional deeper doctor checks

## Non-goals

See README.
