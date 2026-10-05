package pve

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

func TestBackupServiceListRequiresNodeAndStorage(t *testing.T) {
	svc := NewBackupService(&fakeBackend{}, TaskRunner{})

	if _, err := svc.List(context.Background(), BackupListOptions{Storage: "backup"}); err == nil || err.Error() != "node is required" {
		t.Fatalf("node error = %v", err)
	}
	if _, err := svc.List(context.Background(), BackupListOptions{Node: "pve1"}); err == nil || err.Error() != "storage is required" {
		t.Fatalf("storage error = %v", err)
	}
}

func TestBackupServiceListFiltersSortsAndLatest(t *testing.T) {
	backend := &fakeBackend{
		backupRows: map[string]map[string][]output.BackupRow{
			"pve1": {
				"backup": {
					{Node: "pve1", Storage: "backup", Kind: "vm", VMID: 100, CTime: 10, VolID: "backup:backup/vzdump-qemu-100-old.vma.zst"},
					{Node: "pve1", Storage: "backup", Kind: "lxc", VMID: 200, CTime: 30, VolID: "backup:backup/vzdump-lxc-200-new.tar.zst"},
					{Node: "pve1", Storage: "backup", Kind: "vm", VMID: 100, CTime: 20, VolID: "backup:backup/vzdump-qemu-100-new.vma.zst"},
					{Node: "pve1", Storage: "backup", Kind: "vm", VMID: 300, CTime: 15, VolID: "backup:backup/vzdump-qemu-300.vma.zst"},
					{Node: "pve1", Storage: "backup", Kind: "unknown", VMID: 400, CTime: 40, VolID: "backup:backup/unrelated.iso"},
				},
			},
		},
	}
	svc := NewBackupService(backend, TaskRunner{})

	rows, err := svc.List(context.Background(), BackupListOptions{Node: "pve1", Storage: "backup"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertBackupOrder(t, rows, []string{
		"pve1/backup/vm/100/20",
		"pve1/backup/vm/100/10",
		"pve1/backup/lxc/200/30",
		"pve1/backup/vm/300/15",
	})

	rows, err = svc.List(context.Background(), BackupListOptions{Node: "pve1", Storage: "backup", Kind: BackupKindVM})
	if err != nil {
		t.Fatalf("list vm: %v", err)
	}
	assertBackupOrder(t, rows, []string{
		"pve1/backup/vm/100/20",
		"pve1/backup/vm/100/10",
		"pve1/backup/vm/300/15",
	})

	rows, err = svc.List(context.Background(), BackupListOptions{Node: "pve1", Storage: "backup", VMID: 100})
	if err != nil {
		t.Fatalf("list vmid: %v", err)
	}
	assertBackupOrder(t, rows, []string{
		"pve1/backup/vm/100/20",
		"pve1/backup/vm/100/10",
	})

	rows, err = svc.List(context.Background(), BackupListOptions{Node: "pve1", Storage: "backup", Latest: true})
	if err != nil {
		t.Fatalf("list latest: %v", err)
	}
	assertBackupOrder(t, rows, []string{
		"pve1/backup/vm/100/20",
		"pve1/backup/lxc/200/30",
		"pve1/backup/vm/300/15",
	})
}

func TestBackupServiceBackupGuestResolvesVMNodeAndWaits(t *testing.T) {
	task := &fakeTask{upid: "UPID:pve2:backup"}
	backend := &fakeBackend{
		nodes:      []output.NodeRow{{Name: "pve1"}, {Name: "pve2"}},
		vms:        map[string]map[int]*fakeGuest{"pve2": {100: {row: output.GuestRow{Kind: "vm", VMID: 100, Node: "pve2"}}}},
		backupTask: task,
	}
	svc := NewBackupService(backend, TaskRunner{Wait: true})

	result, err := svc.BackupGuest(context.Background(), BackupCreateOptions{
		Kind:    BackupKindVM,
		VMID:    100,
		Storage: "backup",
	})
	if err != nil {
		t.Fatalf("backup guest: %v", err)
	}
	if result.Node != "pve2" || result.Task != "UPID:pve2:backup" || result.Mode != BackupModeSnapshot {
		t.Fatalf("result = %#v", result)
	}
	if backend.vmCalls != 2 || backend.backupNode != "pve2" {
		t.Fatalf("calls node=%s vmCalls=%d", backend.backupNode, backend.vmCalls)
	}
	if backend.backupOptions.Compress != BackupCompressZstd || backend.backupOptions.Mode != BackupModeSnapshot {
		t.Fatalf("backup options = %#v", backend.backupOptions)
	}
	if !task.waited {
		t.Fatal("expected backup task to be waited")
	}
}

func TestBackupServiceBackupGuestPassesLXCOptions(t *testing.T) {
	task := &fakeTask{upid: "UPID:pve1:lxcbackup"}
	backend := &fakeBackend{
		lxcs:       map[string]map[int]*fakeGuest{"pve1": {200: {row: output.GuestRow{Kind: "lxc", VMID: 200, Node: "pve1"}}}},
		backupTask: task,
	}
	svc := NewBackupService(backend, TaskRunner{})

	result, err := svc.BackupGuest(context.Background(), BackupCreateOptions{
		Kind:          BackupKindLXC,
		VMID:          200,
		Node:          "pve1",
		Storage:       "backup",
		Mode:          BackupModeStop,
		Compress:      BackupCompressNone,
		NotesTemplate: "{{guestname}}",
		BwLimit:       1024,
		Protected:     "1",
	})
	if err != nil {
		t.Fatalf("backup guest: %v", err)
	}
	if result.Kind != BackupKindLXC || result.VMID != 200 || result.Node != "pve1" {
		t.Fatalf("result = %#v", result)
	}
	if backend.lxcCalls != 1 || backend.backupCalls != 1 {
		t.Fatalf("lxc calls = %d backup calls = %d", backend.lxcCalls, backend.backupCalls)
	}
	options := backend.backupOptions
	if options.Kind != BackupKindLXC || options.Mode != BackupModeStop || options.Compress != BackupCompressNone ||
		options.NotesTemplate != "{{guestname}}" || options.BwLimit != 1024 || options.Protected != "1" {
		t.Fatalf("backup options = %#v", options)
	}
	if task.waited {
		t.Fatal("did not expect wait without --wait")
	}
}

func TestBackupServiceBackupGuestTaskFailure(t *testing.T) {
	task := &fakeTask{upid: "UPID:pve1:backup", failed: true, exitStatus: "ERROR"}
	backend := &fakeBackend{
		vms:        map[string]map[int]*fakeGuest{"pve1": {100: {row: output.GuestRow{Kind: "vm", VMID: 100, Node: "pve1"}}}},
		backupTask: task,
	}
	svc := NewBackupService(backend, TaskRunner{Wait: true})

	_, err := svc.BackupGuest(context.Background(), BackupCreateOptions{
		Kind:    BackupKindVM,
		VMID:    100,
		Node:    "pve1",
		Storage: "backup",
	})
	if err == nil || err.Error() != "task UPID:pve1:backup failed: ERROR" {
		t.Fatalf("error = %v", err)
	}
}

func TestParseBackupOptions(t *testing.T) {
	if got, err := ParseBackupKind(""); err != nil || got != BackupKindAll {
		t.Fatalf("kind default = %q/%v", got, err)
	}
	if got, err := ParseBackupKind("VM"); err != nil || got != BackupKindVM {
		t.Fatalf("kind vm = %q/%v", got, err)
	}
	if _, err := ParseBackupKind("bad"); err == nil {
		t.Fatal("expected invalid kind")
	}

	if got, err := ParseBackupMode(""); err != nil || got != BackupModeSnapshot {
		t.Fatalf("mode default = %q/%v", got, err)
	}
	if _, err := ParseBackupMode("bad"); err == nil {
		t.Fatal("expected invalid mode")
	}

	if got, err := ParseBackupCompress(""); err != nil || got != BackupCompressZstd {
		t.Fatalf("compress default = %q/%v", got, err)
	}
	if got, err := ParseBackupCompress("none"); err != nil || got != BackupCompressNone {
		t.Fatalf("compress none = %q/%v", got, err)
	}
	if _, err := ParseBackupCompress("bad"); err == nil {
		t.Fatal("expected invalid compression")
	}

	if got, err := ParseBackupProtected("1"); err != nil || got != "1" {
		t.Fatalf("protected = %q/%v", got, err)
	}
	if _, err := ParseBackupProtected("2"); err == nil {
		t.Fatal("expected invalid protected value")
	}
}

func TestMapStorageContentToBackupRowInfersKindAndVMID(t *testing.T) {
	row := mapStorageContentToBackupRow("pve1", "backup", nil)
	if row.Kind != "unknown" {
		t.Fatalf("nil row kind = %q", row.Kind)
	}

	kind := inferBackupKind("backup:backup/vzdump-qemu-100-2026_06_05-10_00_00.vma.zst")
	if kind != BackupKindVM {
		t.Fatalf("kind = %q", kind)
	}
	vmid := inferBackupVMID(0, "backup:backup/vzdump-lxc-200-2026_06_05-10_00_00.tar.zst")
	if vmid != 200 {
		t.Fatalf("vmid = %d", vmid)
	}
	if got := mapBackupCompress(BackupCompressNone); got != "0" {
		t.Fatalf("compress none = %q", got)
	}
}

func assertBackupOrder(t *testing.T, rows []output.BackupRow, want []string) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("rows len = %d, want %d: %#v", len(rows), len(want), rows)
	}
	for i, row := range rows {
		if got := backupKey(row); got != want[i] {
			t.Fatalf("row %d = %s, want %s; rows = %#v", i, got, want[i], rows)
		}
	}
}

func backupKey(row output.BackupRow) string {
	return strings.Join([]string{
		row.Node,
		row.Storage,
		row.Kind,
		strconvFormatUint(row.VMID),
		strconvFormatUint(row.CTime),
	}, "/")
}

func strconvFormatUint(value uint64) string {
	return strconv.FormatUint(value, 10)
}
