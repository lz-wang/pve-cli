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
hosts. Real-PVE acceptance for cross-host migration and backup workflows is
still pending (see the release notes in `docs/roadmap.md`).

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
