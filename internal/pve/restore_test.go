package pve

import (
	"context"
	"errors"
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
	task            Task
	restoreOptions  *RestoreOptions
	restoreTaskUsed Task
}

func (b *fakeRestoreBackend) Restore(_ context.Context, options RestoreOptions) (Task, error) {
	b.restoreOptions = &options
	b.restoreTaskUsed = b.task
	return b.task, nil
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
