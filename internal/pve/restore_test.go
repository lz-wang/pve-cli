package pve

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

func TestRestoreServiceRejectsExistingVMID(t *testing.T) {
	backend := &fakeRestoreBackend{
		fakeBackend: fakeBackend{
			nodes: []output.NodeRow{{Name: "pve1"}},
			vmRows: map[string][]output.GuestRow{
				"pve1": {{Kind: "vm", VMID: 101, Node: "pve1"}},
			},
		},
	}
	svc := NewRestoreService(backend, TaskRunner{})

	_, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "vm", Archive: "backup:backup/vzdump-qemu-100.vma.zst",
		Node: "pve1", VMID: 101,
	})
	if err == nil {
		t.Fatal("expected existing vmid error")
	}
	if backend.restoreOptions != nil {
		t.Fatal("restore should not run when vmid exists")
	}
}

func TestRestoreServiceValidatesInput(t *testing.T) {
	svc := NewRestoreService(&fakeRestoreBackend{}, TaskRunner{})

	cases := []RestoreOptions{
		{Kind: "qemu", Archive: "a", Node: "pve1", VMID: 101},
		{Kind: "vm", Archive: " ", Node: "pve1", VMID: 101},
		{Kind: "vm", Archive: "a", Node: "", VMID: 101},
		{Kind: "vm", Archive: "a", Node: "pve1", VMID: 0},
	}
	for _, options := range cases {
		if _, err := svc.Restore(context.Background(), options); err == nil {
			t.Fatalf("expected validation error for %#v", options)
		}
	}
}

func TestRestoreServiceChecksArchiveKind(t *testing.T) {
	backend := &fakeRestoreBackend{fakeBackend: fakeBackend{nodes: []output.NodeRow{{Name: "pve1"}}}}
	svc := NewRestoreService(backend, TaskRunner{})

	_, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "vm", Archive: "backup:backup/vzdump-lxc-200.tar.zst",
		Node: "pve1", VMID: 101,
	})
	if err == nil {
		t.Fatal("expected archive kind mismatch error")
	}
	if backend.restoreOptions != nil {
		t.Fatal("restore should not run on kind mismatch")
	}
}

func TestRestoreServiceAllowsUnknownArchiveShape(t *testing.T) {
	backend := &fakeRestoreBackend{
		fakeBackend: fakeBackend{
			nodes: []output.NodeRow{{Name: "pve1"}},
			backupRows: map[string]map[string][]output.BackupRow{
				"pve1": {"pbs": {{VolID: "pbs:store/vm/101/2026-01-01T00:00:00Z", Kind: "vm", VMID: 101}}},
			},
		},
		task: &fakeTask{upid: "UPID:pve1:restore"},
	}
	svc := NewRestoreService(backend, TaskRunner{})

	_, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "vm", Archive: "pbs:store/vm/101/2026-01-01T00:00:00Z",
		Node: "pve1", VMID: 101,
	})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
}

func TestRestoreServiceTriggersRestore(t *testing.T) {
	backend := &fakeRestoreBackend{
		fakeBackend: fakeBackend{
			nodes: []output.NodeRow{{Name: "pve1"}},
			backupRows: map[string]map[string][]output.BackupRow{
				"pve1": {"backup": {{VolID: "backup:backup/vzdump-lxc-200.tar.zst", Kind: "lxc", VMID: 200}}},
			},
			storageRows: map[string][]output.StorageRow{
				"pve1": {{Node: "pve1", Storage: "local-lvm", Type: "lvmthin", Active: true, Enabled: true, Content: "rootdir,images", Avail: 100 << 30}},
			},
		},
		task: &fakeTask{upid: "UPID:pve1:restore"},
	}
	svc := NewRestoreService(backend, TaskRunner{})

	result, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "lxc", Archive: "backup:backup/vzdump-lxc-200.tar.zst",
		Node: "pve1", VMID: 201, Storage: "local-lvm",
	})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if result.Task != "UPID:pve1:restore" || result.Kind != "lxc" || result.VMID != 201 {
		t.Fatalf("result = %#v", result)
	}
	if backend.restoreOptions.VMID != 201 || backend.restoreOptions.Storage != "local-lvm" {
		t.Fatalf("restore options = %#v", backend.restoreOptions)
	}
}

func TestRestoreServiceWaitFailurePreservesResult(t *testing.T) {
	backend := &fakeRestoreBackend{
		fakeBackend: fakeBackend{
			nodes: []output.NodeRow{{Name: "pve1"}},
			backupRows: map[string]map[string][]output.BackupRow{
				"pve1": {"backup": {{VolID: "backup:backup/vzdump-qemu-100.vma.zst", Kind: "vm", VMID: 100}}},
			},
		},
		task: &fakeTask{upid: "UPID:pve1:restore", failed: true, exitStatus: "ERROR"},
	}
	svc := NewRestoreService(backend, TaskRunner{Wait: true})

	result, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "vm", Archive: "backup:backup/vzdump-qemu-100.vma.zst",
		Node: "pve1", VMID: 101,
	})
	if err == nil {
		t.Fatal("expected restore task failure")
	}
	// The task was submitted before the wait failed, so the structured
	// result must survive for stdout.
	if result.Task != "UPID:pve1:restore" || result.VMID != 101 || result.Node != "pve1" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRestoreServiceVMIDCheckFailsClosedOnNodeFailure(t *testing.T) {
	backend := &fakeRestoreBackend{
		fakeBackend: fakeBackend{
			nodes:   []output.NodeRow{{Name: "pve1"}, {Name: "pve2"}},
			vmErrs:  map[string]error{"pve1": errors.New("timeout")},
			lxcErrs: map[string]error{"pve1": errors.New("timeout")},
			vmRows:  map[string][]output.GuestRow{"pve2": {{Kind: "vm", VMID: 100, Node: "pve2"}}},
			backupRows: map[string]map[string][]output.BackupRow{
				"pve2": {"backup": {{VolID: "backup:backup/vzdump-qemu-100.vma.zst", Kind: "vm", VMID: 100}}},
			},
		},
		task: &fakeTask{upid: "UPID:pve2:restore"},
	}
	svc := NewRestoreService(backend, TaskRunner{})

	// An unverifiable node inventory must abort the restore even though the
	// target VMID looks free on the reachable node.
	_, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "vm", Archive: "backup:backup/vzdump-qemu-100.vma.zst",
		Node: "pve2", VMID: 105,
	})
	if err == nil {
		t.Fatal("expected restore to fail closed when a node inventory cannot be verified")
	}
	if !strings.Contains(err.Error(), "pve1") {
		t.Fatalf("error = %v", err)
	}
	if backend.restoreOptions != nil {
		t.Fatal("restore must not run when the vmid check is incomplete")
	}

	// A fully queryable cluster still allows free VMIDs.
	backend.vmErrs = nil
	backend.lxcErrs = nil
	if _, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "vm", Archive: "backup:backup/vzdump-qemu-100.vma.zst",
		Node: "pve2", VMID: 105,
	}); err != nil {
		t.Fatalf("restore free vmid: %v", err)
	}
}

type fakeRestoreBackend struct {
	fakeBackend
	task             Task
	restoreOptions   *RestoreOptions
	restoreTaskUsed  Task
	restoreSubmitErr error
}

func (b *fakeRestoreBackend) Restore(_ context.Context, options RestoreOptions) (Task, error) {
	b.restoreOptions = &options
	b.restoreTaskUsed = b.task
	return b.task, b.restoreSubmitErr
}

// P1-4: a restore task that succeeded while the post-restore configuration
// (bridge mapping) failed must not report the whole operation succeeded.
// The split states tell the automation exactly what happened: the archive
// restored (restore_status succeeded), the bridge rewrite failed
// (post_config_status failed), the overall operation is partial — and the
// error keeps the exit code non-zero.
func TestRestoreServicePartialWhenPostConfigFails(t *testing.T) {
	backend := &fakeRestoreBackend{
		fakeBackend: fakeBackend{
			nodes: []output.NodeRow{{Name: "pve1"}},
			backupRows: map[string]map[string][]output.BackupRow{
				"pve1": {"backup": {{VolID: "backup:backup/vzdump-lxc-200.tar.zst", Kind: "lxc", VMID: 200}}},
			},
			storageRows: map[string][]output.StorageRow{
				"pve1": {{Node: "pve1", Storage: "local-lvm", Type: "lvmthin", Active: true, Enabled: true, Content: "rootdir", Avail: 100 << 30}},
			},
			networkRows: map[string][]output.NetworkRow{
				"pve1": {{Node: "pve1", Name: "vmbr0", Type: "bridge", Active: true}},
			},
			// A restored guest exists, but this fake has no native config
			// capability, so the bridge mapping cannot apply.
			lxcs: map[string]map[int]*fakeGuest{"pve1": {201: {row: output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve1", Status: "stopped"}}}},
		},
		task: &fakeTask{upid: "UPID:pve1:restore"},
	}
	svc := NewRestoreService(backend, TaskRunner{Wait: true, WaitTimeout: time.Second})

	result, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "lxc", Archive: "backup:backup/vzdump-lxc-200.tar.zst",
		Node: "pve1", VMID: 201, Storage: "local-lvm",
		BridgeMap: map[string]string{"vmbr0": "vmbr0"},
	})
	if err == nil {
		t.Fatal("post-restore configuration failure must fail the command")
	}
	if result.Status != "partial" {
		t.Fatalf("status = %q, want partial (archive restored, configuration failed)", result.Status)
	}
	if result.RestoreStatus != "succeeded" {
		t.Fatalf("restore_status = %q, want succeeded", result.RestoreStatus)
	}
	if result.PostConfigStatus != "failed" {
		t.Fatalf("post_config_status = %q, want failed", result.PostConfigStatus)
	}
	if result.Task != "UPID:pve1:restore" {
		t.Fatalf("task = %q, want the preserved UPID", result.Task)
	}
}

// P1-4: the happy path reports every split state: the archive restored, a
// requested bridge mapping applied, the operation succeeded. Without a
// mapping the post-configuration is skipped, not silently "succeeded".
func TestRestoreServiceSplitStates(t *testing.T) {
	newBackend := func() *fakeRestoreBackend {
		return &fakeRestoreBackend{
			fakeBackend: fakeBackend{
				nodes: []output.NodeRow{{Name: "pve1"}},
				backupRows: map[string]map[string][]output.BackupRow{
					"pve1": {"backup": {{VolID: "backup:backup/vzdump-lxc-200.tar.zst", Kind: "lxc", VMID: 200}}},
				},
				storageRows: map[string][]output.StorageRow{
					"pve1": {{Node: "pve1", Storage: "local-lvm", Type: "lvmthin", Active: true, Enabled: true, Content: "rootdir", Avail: 100 << 30}},
				},
				networkRows: map[string][]output.NetworkRow{
					"pve1": {{Node: "pve1", Name: "vmbr0", Type: "bridge", Active: true}},
				},
				lxcs: map[string]map[int]*fakeGuest{"pve1": {201: {row: output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve1", Status: "stopped"}}}},
			},
			task: &fakeTask{upid: "UPID:pve1:restore"},
		}
	}

	// No bridge mapping: post-configuration skipped.
	svc := NewRestoreService(newBackend(), TaskRunner{Wait: true, WaitTimeout: time.Second})
	result, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "lxc", Archive: "backup:backup/vzdump-lxc-200.tar.zst",
		Node: "pve1", VMID: 201, Storage: "local-lvm",
	})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if result.Status != "succeeded" || result.RestoreStatus != "succeeded" || result.PostConfigStatus != "skipped" {
		t.Fatalf("result = %#v, want succeeded/succeeded/skipped", result)
	}

	// A failed restore task: post-configuration never ran.
	failed := newBackend()
	failed.task = &fakeTask{upid: "UPID:pve1:restore", failed: true, exitStatus: "ERROR"}
	svc = NewRestoreService(failed, TaskRunner{Wait: true, WaitTimeout: time.Second})
	result, err = svc.Restore(context.Background(), RestoreOptions{
		Kind: "lxc", Archive: "backup:backup/vzdump-lxc-200.tar.zst",
		Node: "pve1", VMID: 201, Storage: "local-lvm",
		BridgeMap: map[string]string{"vmbr0": "vmbr0"},
	})
	if err == nil {
		t.Fatal("expected restore task failure")
	}
	if result.RestoreStatus != "failed" || result.PostConfigStatus != "skipped" {
		t.Fatalf("result = %#v, want restore_status failed / post_config skipped", result)
	}
}

// bridgeMapBackend adds native config reads to a restore backend: the
// restored guest carries a native configuration with one net device.
type bridgeMapBackend struct {
	*fakeRestoreBackend
	guestConfigs map[string]map[string]any
}

// P1-2: a restore submission failure that does not prove PVE rejected the
// request keeps the outcome unknown and any returned UPID — the restore may
// still be running, and rerunning blindly could duplicate a real restore.
func TestRestoreSubmissionFailureKeepsUnknown(t *testing.T) {
	backend := &fakeRestoreBackend{
		fakeBackend: fakeBackend{
			nodes: []output.NodeRow{{Name: "pve1"}},
			backupRows: map[string]map[string][]output.BackupRow{
				"pve1": {"backup": {{VolID: "backup:backup/vzdump-lxc-200.tar.zst", Kind: "lxc", VMID: 200}}},
			},
			storageRows: map[string][]output.StorageRow{
				"pve1": {{Node: "pve1", Storage: "local-lvm", Type: "lvmthin", Active: true, Enabled: true, Content: "rootdir", Avail: 100 << 30}},
			},
		},
		task:             &fakeTask{upid: "UPID:pve1:restore"},
		restoreSubmitErr: &APIError{HTTPStatus: 500, Category: CategoryUnavailable, Message: "internal error", kind: ErrUnavailable},
	}
	svc := NewRestoreService(backend, TaskRunner{Wait: true})

	result, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "lxc", Archive: "backup:backup/vzdump-lxc-200.tar.zst",
		Node: "pve1", VMID: 201, Storage: "local-lvm",
	})
	if err == nil {
		t.Fatal("a failed submission must fail the command")
	}
	if result.Resource != "lxc" || result.PostConfigStatus != "skipped" {
		t.Fatalf("incomplete submission result: %+v", result)
	}
	if result.Status != "unknown" || result.RestoreStatus != "unknown" {
		t.Fatalf("status = %q/%q, want unknown/unknown (submission outcome unproven)", result.Status, result.RestoreStatus)
	}
	// P1-4: the unconfirmed outcome must exit 5 (cannot safely retry), not
	// the transport kind's 3 (transient).
	if !errors.Is(err, ErrUncertain) {
		t.Fatalf("err = %v, want ErrUncertain (exit code 5 contract)", err)
	}
	if result.Task != "UPID:pve1:restore" {
		t.Fatalf("task = %q, want the UPID returned alongside the error", result.Task)
	}
}

// P1-2: a provable submission rejection (HTTP 4xx except 408) reports
// failed — PVE refused the restore before it started, so a corrected rerun
// is safe.
func TestRestoreSubmissionRejectionReportsFailed(t *testing.T) {
	backend := &fakeRestoreBackend{
		fakeBackend: fakeBackend{
			nodes: []output.NodeRow{{Name: "pve1"}},
			backupRows: map[string]map[string][]output.BackupRow{
				"pve1": {"backup": {{VolID: "backup:backup/vzdump-lxc-200.tar.zst", Kind: "lxc", VMID: 200}}},
			},
			storageRows: map[string][]output.StorageRow{
				"pve1": {{Node: "pve1", Storage: "local-lvm", Type: "lvmthin", Active: true, Enabled: true, Content: "rootdir", Avail: 100 << 30}},
			},
		},
		task:             &fakeTask{upid: "UPID:pve1:restore"},
		restoreSubmitErr: &APIError{HTTPStatus: 400, Category: CategoryInvalid, Message: "bad archive", kind: ErrInvalid},
	}
	svc := NewRestoreService(backend, TaskRunner{Wait: true})

	result, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "lxc", Archive: "backup:backup/vzdump-lxc-200.tar.zst",
		Node: "pve1", VMID: 201, Storage: "local-lvm",
	})
	if err == nil {
		t.Fatal("a rejected submission must fail the command")
	}
	if result.Resource != "lxc" || result.PostConfigStatus != "skipped" {
		t.Fatalf("incomplete submission result: %+v", result)
	}
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed (provable rejection)", result.Status)
	}
	// P1-4: a provable rejection keeps its classified exit code — it must
	// not read as uncertain.
	if errors.Is(err, ErrUncertain) {
		t.Fatalf("a provable rejection must not report uncertain: %v", err)
	}
}

// GuestConfig serves the config keyed "kind/node/vmid"; the map is returned
// as-is so a fakeGuest.onConfig hook can mutate it to simulate a read-back.
func (b *bridgeMapBackend) GuestConfig(_ context.Context, kind, node string, id int) (map[string]any, error) {
	cfg, ok := b.guestConfigs[kind+"/"+node+"/"+strconv.Itoa(id)]
	if !ok {
		return map[string]any{}, nil
	}
	return cfg, nil
}

func bridgeMapBackendOf(digest, net0 string) *bridgeMapBackend {
	backend := &bridgeMapBackend{
		fakeRestoreBackend: &fakeRestoreBackend{
			fakeBackend: fakeBackend{
				nodes: []output.NodeRow{{Name: "pve1"}},
				backupRows: map[string]map[string][]output.BackupRow{
					"pve1": {"backup": {{VolID: "backup:backup/vzdump-lxc-200.tar.zst", Kind: "lxc", VMID: 200}}},
				},
				storageRows: map[string][]output.StorageRow{
					"pve1": {{Node: "pve1", Storage: "local-lvm", Type: "lvmthin", Active: true, Enabled: true, Content: "rootdir", Avail: 100 << 30}},
				},
				networkRows: map[string][]output.NetworkRow{
					"pve1": {{Node: "pve1", Name: "vmbr0", Type: "bridge", Active: true}},
				},
				lxcs: map[string]map[int]*fakeGuest{"pve1": {201: {
					row:  output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve1", Status: "stopped"},
					task: &fakeTask{upid: "UPID:pve1:config"},
				}}},
			},
			task: &fakeTask{upid: "UPID:pve1:restore"},
		},
		guestConfigs: map[string]map[string]any{
			"lxc/pve1/201": {
				"digest": digest,
				"net0":   net0,
			},
		},
	}
	return backend
}

// P1-2: an explicit bridge mapping that matches no net device on the
// restored guest is a configuration error, never a silent no-op — the
// command fails without submitting any config write.
func TestRestoreBridgeMapUnmatchedSourceFails(t *testing.T) {
	backend := bridgeMapBackendOf("digest-1", "name=eth0,bridge=vmbr2,firewall=1")
	guest := backend.fakeRestoreBackend.lxcs["pve1"][201]
	svc := NewRestoreService(backend, TaskRunner{Wait: true, WaitTimeout: time.Second})

	result, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "lxc", Archive: "backup:backup/vzdump-lxc-200.tar.zst",
		Node: "pve1", VMID: 201, Storage: "local-lvm",
		BridgeMap: map[string]string{"vmbr9": "vmbr0"},
	})
	if err == nil {
		t.Fatal("a mapping matching no device must fail the command")
	}
	if result.Status != "partial" || result.PostConfigStatus != "failed" {
		t.Fatalf("result = %#v, want partial/failed (archive restored, mapping rejected)", result)
	}
	if guest.configCalls != 0 {
		t.Fatal("an unmatched mapping must not submit any config write")
	}
}

// P1-2: the mapping verifies the applied state by reading the configuration
// back: a config write the API accepted that did not change the bridge can
// never report post_config_status succeeded.
func TestRestoreBridgeMapVerifiesAppliedState(t *testing.T) {
	backend := bridgeMapBackendOf("digest-1", "name=eth0,bridge=vmbr2,firewall=1")
	svc := NewRestoreService(backend, TaskRunner{Wait: true, WaitTimeout: time.Second})

	result, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "lxc", Archive: "backup:backup/vzdump-lxc-200.tar.zst",
		Node: "pve1", VMID: 201, Storage: "local-lvm",
		BridgeMap: map[string]string{"vmbr2": "vmbr0"},
	})
	if err == nil {
		t.Fatal("a mapping that did not take effect must fail the command")
	}
	if result.Status != "partial" || result.PostConfigStatus != "failed" {
		t.Fatalf("result = %#v, want partial/failed (write accepted, state unchanged)", result)
	}
}

// P1-2: the config write submits under the digest read with the
// configuration, so a concurrent write between read and submit is rejected
// by PVE instead of silently overwritten. A mapping that took effect
// reports post_config_status succeeded.
func TestRestoreBridgeMapSubmitsWithDigest(t *testing.T) {
	backend := bridgeMapBackendOf("digest-1", "name=eth0,bridge=vmbr2,firewall=1")
	guest := backend.fakeRestoreBackend.lxcs["pve1"][201]
	guest.onConfig = func(values map[string]string) {
		config := backend.guestConfigs["lxc/pve1/201"]
		for key, value := range values {
			config[key] = value
		}
	}
	svc := NewRestoreService(backend, TaskRunner{Wait: true, WaitTimeout: time.Second})

	result, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "lxc", Archive: "backup:backup/vzdump-lxc-200.tar.zst",
		Node: "pve1", VMID: 201, Storage: "local-lvm",
		BridgeMap: map[string]string{"vmbr2": "vmbr0"},
	})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if result.Status != "succeeded" || result.RestoreStatus != "succeeded" || result.PostConfigStatus != "succeeded" {
		t.Fatalf("result = %#v, want all three succeeded", result)
	}
	if guest.configDigest != "digest-1" {
		t.Fatalf("submitted digest = %q, want the digest read with the config", guest.configDigest)
	}
}

// P1-1: --bridge-map without --wait is an invalid argument combination and
// must be rejected before the restore is submitted: backend.Restore is never
// called, no task/UPID exists, and the target resource is untouched. The
// caller sees a plain argument error (exit code 2) instead of a submitted
// restore hiding behind a non-zero exit.
func TestRestoreBridgeMapRequiresWait(t *testing.T) {
	backend := bridgeMapBackendOf("digest-1", "name=eth0,bridge=vmbr2,firewall=1")
	svc := NewRestoreService(backend, TaskRunner{Wait: false})

	result, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "lxc", Archive: "backup:backup/vzdump-lxc-200.tar.zst",
		Node: "pve1", VMID: 201, Storage: "local-lvm",
		BridgeMap: map[string]string{"vmbr2": "vmbr0"},
	})
	if err == nil {
		t.Fatal("a bridge map without --wait must fail the command")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid (exit code 2)", err)
	}
	if backend.restoreOptions != nil {
		t.Fatal("restore must never be submitted for a rejected argument combination")
	}
	if result.Task != "" {
		t.Fatalf("task = %q, want no UPID: nothing was submitted", result.Task)
	}
	if result.Status != "" {
		t.Fatalf("status = %q, want empty: the operation never started", result.Status)
	}
}

// Expose only Guest to model a backend without digest submission support.
type guestWithoutDigest struct{ Guest }

type restoreWithoutDigestBackend struct{ *bridgeMapBackend }

func (b *restoreWithoutDigestBackend) LXC(ctx context.Context, node string, id int) (Guest, error) {
	guest, err := b.bridgeMapBackend.LXC(ctx, node, id)
	return guestWithoutDigest{guest}, err
}

func TestRestoreBridgeMapRequiresDigestProtection(t *testing.T) {
	for _, tc := range []struct {
		name        string
		digest      any
		unsupported bool
	}{
		{"missing", nil, false},
		{"empty", "", false},
		{"whitespace", "  ", false},
		{"wrong_type", 123, false},
		{"unsupported", "digest-1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := bridgeMapBackendOf("digest-1", "name=eth0,bridge=vmbr2")
			if tc.digest == nil {
				delete(backend.guestConfigs["lxc/pve1/201"], "digest")
			} else {
				backend.guestConfigs["lxc/pve1/201"]["digest"] = tc.digest
			}
			guest := backend.lxcs["pve1"][201]
			var restoreBackend RestoreBackend = backend
			if tc.unsupported {
				restoreBackend = &restoreWithoutDigestBackend{backend}
			}
			svc := NewRestoreService(restoreBackend, TaskRunner{Wait: true, WaitTimeout: time.Second})
			result, err := svc.Restore(context.Background(), RestoreOptions{
				Kind: "lxc", Archive: "backup:backup/vzdump-lxc-200.tar.zst",
				Node: "pve1", VMID: 201, Storage: "local-lvm", BridgeMap: map[string]string{"vmbr2": "vmbr0"},
			})
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("err=%v, want unavailable", err)
			}
			if result.Status != "partial" || result.RestoreStatus != "succeeded" || result.PostConfigStatus != "failed" || result.Task != "UPID:pve1:restore" || result.Error == "" {
				t.Fatalf("result=%+v", result)
			}
			if guest.configCalls != 0 {
				t.Fatalf("unprotected config writes=%d", guest.configCalls)
			}
		})
	}
}
