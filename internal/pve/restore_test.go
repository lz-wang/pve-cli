package pve

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v2/internal/output"
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
	backend := &fakeRestoreBackend{fakeBackend: fakeBackend{nodes: []output.NodeRow{{Name: "pve1"}}}}
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
		fakeBackend: fakeBackend{nodes: []output.NodeRow{{Name: "pve1"}}},
		task:        &fakeTask{upid: "UPID:pve1:restore"},
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
		fakeBackend: fakeBackend{nodes: []output.NodeRow{{Name: "pve1"}}},
		task:        &fakeTask{upid: "UPID:pve1:restore", failed: true, exitStatus: "ERROR"},
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
