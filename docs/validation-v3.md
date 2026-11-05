# v3.0.0 validation record

Validation date: 2026-10-29. Ordinary tests do not require real credentials.
Post-v3 capability validation (P0–P4, unreleased) is recorded at the end.

## Local verification

- `go fmt ./...`, `go vet ./...`, `go test ./...`, `go test -race ./...` and
  native build: passed.
- `golangci-lint run`: 0 issues.
- `make docs-check`, `git diff --check` and workflow `actionlint`: passed.
- Linux/macOS/Windows amd64/arm64 build matrix and all six SHA-256 checks:
  passed. These cross-builds prove compilation, not execution on each OS.
- Bash and Zsh completion syntax plus static/dynamic completion contract tests:
  passed. Fish script generation/contract tests passed; the local host has no
  Fish runtime. CI installs all three shells for syntax validation.
- HTTP contracts cover creation, disk unlink parameters, template download,
  node power, error classification, secret-safe errors and no mutation retries.
- Service/command tests cover incomplete resolution, ambiguous names, cancelled
  work, target limits, snapshot previews/preconditions, confirmation, credential
  permissions and structured task outcomes.

## Real PVE acceptance

The saved 8845h API profile was used through a temporary private v3 credential
reference; its original config and credentials were not rewritten.

Dedicated VM 990001 and LXC 990002 were created stopped, using small disks and
an existing Debian 13 system template. Verification covered:

- Doctor, guest inventory, exact-name resolution and native configuration.
- VM and unprivileged LXC creation, with native configuration changes.
- VM disk allocation, detach into unused0, independent storage proof that the
  detached volume remained, then unused-volume deletion and absence checks.
- VM snapshot creation, rollback dry-run, rollback with memory-state proof and
  deletion; LXC snapshot creation, delete dry-run and deletion.
- Two-worker mixed VM/LXC bulk start and stop, both returning succeeded.
- Strict health checks, complete status report and exit code 4 for an explicitly
  missing VM on a selected node.

Both test guests were deleted after acceptance. Independent final inventory
and storage reads confirmed that their volumes were gone, only the original
VM remained, and that VM's native configuration matched its pre-test snapshot.

Template download and task cancellation were verified with local HTTP/mock
contracts, without creating a real download/cancellation workload. Node reboot
and shutdown were deliberately verified only with mock APIs; no physical host
power operation was performed. Backup/restore/clone exceptional paths are
covered locally; no destructive production recovery drill was performed.

## Offline maintenance lifecycle acceptance (P4)

Local lifecycle tests validate the full state machine against a simulated
host: mount → read a file through the mount → lock observable in the guest
config while mounted → unmount → lock released; interrupted mounts report
failure with the manual-cleanup hint and recover through `pct unmount`;
unmounts that claim success while the mount persists report `unknown`,
fail the command with exit 5, and a retry completes the release; double
mounts are refused without touching the mounted state; a mount-state probe
that cannot determine the state is an error, never a silent "not mounted".

The real-PVE acceptance procedure below must run on a **dedicated test
container only** — mount holds container locks and interrupted mounts can
need host-side cleanup. Never run it against production guests.

1. Create a disposable stopped LXC (for example from a system template).
2. `pve lxc mount 201 --node <node>` — expect `succeeded`, the reported
   host path, `mounted: true`; on the host, read a file through the path
   (for example `cat <path>/etc/hostname`).
3. While mounted, `pve lxc get 201` must show the container locked; a
   start attempt must fail until unmounted.
4. `pve lxc mount 201 --node <node>` again — expect the double-mount
   refusal (exit 4) with the mounted state untouched.
5. Interrupt a run (Ctrl+C during mount) — expect an `unknown`/`failed`
   outcome with the manual-cleanup hint and a non-zero exit; recover with
   `pve lxc unmount 201 --node <node>` until it reports released.
6. `pve lxc unmount 201 --node <node>` — expect `succeeded`,
   `mounted: false`, the lock gone, and the container startable again.
7. Delete the test container and confirm its volumes are gone.

## Post-review reliability regression coverage (2026-11-04)

The following contract regressions were added after the reliability review;
all run against HTTP contract servers or scripted transports, not real
hosts. Real-PVE acceptance was pending at that review. The 2026-11-06 record below
covers backup workflows. Remote migration acceptance remains separate.

- Remote migration: HTTP 400/500/timeout/nil-task/empty-UPID submissions
  never report `succeeded`; the structured result keeps `failed`/`unknown`
  and the original error.
- Remote migration wire format: `target-endpoint` uses PVE's official
  comma-separated attributes (`apitoken=…,host=…,port=…,fingerprint=…`);
  storage/bridge maps are submitted as `source:target` pairs.
- Secret protection: a source-side 400 that echoes the submitted endpoint
  bundle must not leak the target token into stdout, stderr or the error
  (CLI-chain regression); endpoint-bearing field errors are redacted whole.
- Inventory fail-closed: an unreadable target guest inventory fails the
  migration plan's VMID check (never "vmid free") and refuses the
  copy-restore workflow.
- SSH node binding: `lxc exec`/`lxc mount`/`lxc unmount` refuse pct
  commands when the profile's `ssh.node` is missing or disagrees with the
  API-resolved guest node.
- Workflow targeting: an existing target guest conflicts instead of being
  treated as a restored resource; the backup stage only accepts archives
  created during the run (clock-skew tolerated).
- Restore reporting: a succeeded restore task with a failed post-restore
  bridge mapping reports the stage as `partial` and fails the workflow;
  post-check warnings propagate as an overall `warning`.
- Transfer publication: hard-link publish (no overwrite window), concurrent
  publish conflicts, checksum-identical existing files continue
  idempotently, different files conflict.
- Mount outcomes: unconfirmed mount/unmount states exit 5 with
  `status=unknown` kept in the structured result.

## Backup/restore review follow-up (2026-11-06)

Local verification passed: `make check test test-race build docs-check`
and `git diff --check`.

- Submission results retain the resource type on rejected and uncertain
  outcomes. Backup reports `effective_mode=unknown`. Restore reports
  `post_config_status=skipped`. Service tests also verify retained UPIDs.
- Sixteen CLI/HTTP cases cover VM and LXC backup and restore submissions.
  HTTP 400 returns `failed` and exit 2. HTTP 500, HTTP 408 and client
  timeouts return `unknown` and exit 5. Each case verifies one submission,
  structured error fields and no invented task ID.
- Restore bridge mapping requires a non-empty string digest and support
  for digest-protected updates. Missing, empty, whitespace and non-string
  digests, and unavailable update support, cause no configuration writes.
  The result retains the restore UPID and reports a partial restore.
  Existing tests verify successful digest submission and configuration read-back.

These checks use local HTTP servers and fake backends. No real PVE resources
were created, copied, restored or deleted during this follow-up.
The subsequent real-PVE acceptance is recorded below.


## Real cross-host LXC backup/restore acceptance (2026-11-06)

Acceptance ran at 00:41–00:48 UTC+8 between profiles `8845h` and `8600k`.
Live API discovery confirmed nodes `pve-8845h` and `pve-8600k`.
The tested code was `08d305d` plus the storage-selection fix committed as
`97437bc`. Local checks passed: `make check test test-race docs-check`,
`make build` and `git diff --check`.

### Test resources and scope

The source used the existing Debian 13 standard template, one CPU,
256 MiB memory, no swap and a 2 GiB unprivileged root filesystem.
All three test containers used hostname `pve-cli-accept-20261106`.
No SSH public key was injected. No container was started.

| Profile / node | CTID | Role | Root filesystem |
| --- | --- | --- | --- |
| `8845h / pve-8845h` | 298 | Original source | `local-lvm:vm-298-disk-0` |
| `8600k / pve-8600k` | 299 | Forward restore and reverse source | `local-lvm:vm-299-disk-0` |
| `8845h / pve-8845h` | 297 | Reverse restore | `local-lvm:vm-297-disk-0` |

The IDs were free before creation or restore. Both nodes had sufficient
space on `local` for archives and `local-lvm` for guest disks.
Each node had one bridge, `vmbr0`. The test explicitly supplied
`--bridge-map vmbr0=vmbr0`, exercising digest-protected submission and
configuration read-back. Different-name bridge rewriting was not tested
on these hosts.

The restored network retained `ip=manual,ip6=manual` and the source MAC.
All guests remained stopped, with no configuration lock and autostart disabled.
No DHCP lease or network service was requested.

### Discovered defect and successful continuation

The first forward run completed backup and checksum-verified transfer.
Restore then returned HTTP 400 because PVE selected `local`, which did not
support container directories. No restore task or target guest was created.
The CLI reported a failed restore stage and exit 2.

Commit `97437bc` adds `--restore-storage` for guest disks, independently of
`--target-storage` for the archive. Command and service regressions cover
explicit disk storage and the unchanged omitted-option behavior.

The forward continuation used the exact existing archive with `--archive`
and `--restore-storage local-lvm`. It verified the existing target archive
checksum and restored CTID 299. The reverse run created a new backup of
CTID 299, copied it to 8845h and restored CTID 297 with the same disk option.

| Direction | Backup | Transfer | Restore | Post-check / overall |
| --- | --- | --- | --- | --- |
| 8845h:298 → 8600k:299 | Succeeded initially, skipped on continuation | Succeeded | Succeeded | Warning |
| 8600k:299 → 8845h:297 | Succeeded | Succeeded | Succeeded | Warning |

Both workflows returned exit 0. Their warnings explicitly named unconfirmed
`mac_ip` and `source` checks. The source containers remained present until
cleanup. API reads confirmed the restored resources, storage and network
configuration. The warnings were not reported as unconditional safety.

### Archive and task evidence

Both archives were stored under `/var/lib/vz/dump/` on each host.
A subsequent `backup copy` in each direction returned `succeeded` and
`resumed=true`, confirming checksum-identical reuse without another transfer.

| Archive filename | Bytes | SHA-256 |
| --- | ---: | --- |
| `vzdump-lxc-298-2026_11_06-00_42_26.tar.zst` | 167048169 | `ee70bef51fc325ee2a7a4ef4a859e17bf7666c497675677ae39dbab5ac5c17e6` |
| `vzdump-lxc-299-2026_11_06-00_45_05.tar.zst` | 167127244 | `b65f8859d287fd868980e5b930afb5b8bcf469895ca36a0570b524dcf74e00b0` |

Independent `task get` and `task log` queries confirmed these tasks.
Each task returned `exit_status=OK`.

| Operation | UPID |
| --- | --- |
| Create 298 | `UPID:pve-8845h:001718DB:6424F859:6AECB1D4:vzcreate:298:root@pam!pvectl:` |
| Backup 298 | `UPID:pve-8845h:001719AC:64250418:6AECB1F2:vzdump:298:root@pam!pvectl:` |
| Restore 299 | `UPID:pve-8600k:00002344:000377B5:6AECB26A:vzrestore:299:root@pam!pve-cli:` |
| Backup 299 | `UPID:pve-8600k:000023F4:000386D4:6AECB291:vzdump:299:root@pam!pve-cli:` |
| Restore 297 | `UPID:pve-8845h:00171DD3:64254793:6AECB29F:vzrestore:297:root@pam!pvectl:` |

### Cleanup and remaining limits

The CLI deleted CTIDs 297 and 298 on 8845h and CTID 299 on 8600k.
Each delete used the verified node, ID and test hostname.
All three deletion tasks completed with `exit_status=OK`.
Fresh API inventories confirmed that the guests and their root volumes
were absent.

The CLI has no archive deletion command. With explicit user authorization,
SSH removed only the four verified archive copies and two matching backup logs.
The cleanup checked exact filenames, file sizes, file types and archive
checksums before deletion. No transfer part files remained.

The final guest identity/status lists and `local` storage content lists
matched their pre-test baselines. Existing guests retained their initial
running or stopped states. HomeLab `hosts.yml` and `domains.yml` required
no changes: no persistent guest, IP assignment, service or domain remained.
No DHCP configuration was deployed.

This acceptance covers stopped LXC backup, bidirectional archive transfer,
restore, same-name bridge mapping, task inspection and complete cleanup.
It does not cover guest startup, application health, different-name bridges,
remote migration, or real network fault injection. Timeout and HTTP error
classification remain covered by local contract tests.
