package pve

import (
	"context"
	"errors"
	"testing"

	"github.com/lz-wang/pvectl/internal/output"
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
	svc := NewRestoreService(backend, TaskRunner{}, nil, false)

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
	svc := NewRestoreService(&fakeRestoreBackend{}, TaskRunner{}, nil, false)

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
	svc := NewRestoreService(backend, TaskRunner{}, nil, false)

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
	svc := NewRestoreService(backend, TaskRunner{}, nil, false)

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
	svc := NewRestoreService(backend, TaskRunner{}, nil, false)

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

func TestRestoreServiceWaitFailurePropagates(t *testing.T) {
	backend := &fakeRestoreBackend{
		fakeBackend: fakeBackend{nodes: []output.NodeRow{{Name: "pve1"}}},
		task:        &fakeTask{upid: "UPID:pve1:restore", failed: true, exitStatus: "ERROR"},
	}
	svc := NewRestoreService(backend, TaskRunner{Wait: true}, nil, false)

	_, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "vm", Archive: "backup:backup/vzdump-qemu-100.vma.zst",
		Node: "pve1", VMID: 101,
	})
	if err == nil {
		t.Fatal("expected restore task failure")
	}
}

func TestRestoreServiceVMIDCheckToleratesNodeFailure(t *testing.T) {
	backend := &fakeRestoreBackend{
		fakeBackend: fakeBackend{
			nodes:   []output.NodeRow{{Name: "pve1"}, {Name: "pve2"}},
			vmErrs:  map[string]error{"pve1": errors.New("timeout")},
			lxcErrs: map[string]error{"pve1": errors.New("timeout")},
			vmRows:  map[string][]output.GuestRow{"pve2": {{Kind: "vm", VMID: 100, Node: "pve2"}}},
		},
		task: &fakeTask{upid: "UPID:pve2:restore"},
	}
	svc := NewRestoreService(backend, TaskRunner{}, nil, false)

	if _, err := svc.Restore(context.Background(), RestoreOptions{
		Kind: "vm", Archive: "backup:backup/vzdump-qemu-100.vma.zst",
		Node: "pve2", VMID: 100,
	}); err == nil {
		t.Fatal("expected vmid 100 to be detected on pve2 despite pve1 failure")
	}

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
