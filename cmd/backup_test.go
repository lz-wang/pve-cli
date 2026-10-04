package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lz-wang/pvectl/internal/output"
)

func TestBackupListCommandWritesJSONAndFiltersLatest(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	backend := &commandBackend{
		backups: map[string]map[string][]output.BackupRow{
			"pve1": {
				"backup": {
					{Node: "pve1", Storage: "backup", Kind: "vm", VMID: 100, CTime: 10, VolID: "backup:backup/vzdump-qemu-100-old.vma.zst"},
					{Node: "pve1", Storage: "backup", Kind: "vm", VMID: 100, CTime: 20, VolID: "backup:backup/vzdump-qemu-100-new.vma.zst"},
					{Node: "pve1", Storage: "backup", Kind: "lxc", VMID: 200, CTime: 30, VolID: "backup:backup/vzdump-lxc-200.tar.zst"},
				},
			},
		},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"backup", "ls",
		"--node", "pve1",
		"--storage", "backup",
		"--kind", "vm",
		"--latest",
		"-o", "json",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, `"vmid": 100`) || !strings.Contains(got, "new") || strings.Contains(got, "old") || strings.Contains(got, `"kind": "lxc"`) {
		t.Fatalf("stdout = %s", got)
	}
}

func TestBackupListCommandRejectsInvalidKind(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")

	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"backup", "ls",
		"--node", "pve1",
		"--storage", "backup",
		"--kind", "bad",
	}, "test", testDeps(&bytes.Buffer{}, &commandBackend{}))
	if err == nil || err.Error() != `invalid backup kind "bad", expected all, vm, or lxc` {
		t.Fatalf("error = %v", err)
	}
}

func TestVMBackupCommandWritesResultAndWaits(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	task := &commandTask{upid: "UPID:pve2:backup"}
	backend := &commandBackend{
		nodes:      []output.NodeRow{{Name: "pve1"}, {Name: "pve2"}},
		vmGuests:   map[string]map[int]*commandGuest{"pve2": {100: {row: output.GuestRow{Kind: "vm", VMID: 100, Node: "pve2"}}}},
		backupTask: task,
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"vm", "backup", "100",
		"--storage", "backup",
		"--mode", "stop",
		"--compress", "none",
		"--notes-template", "{{guestname}}",
		"--bwlimit", "2048",
		"--protected", "1",
		"--wait",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, `"task": "UPID:pve2:backup"`) || !strings.Contains(got, `"mode": "stop"`) {
		t.Fatalf("stdout = %s", got)
	}
	if backend.backupNode != "pve2" || backend.backupOptions.Compress != "none" || backend.backupOptions.BwLimit != 2048 ||
		backend.backupOptions.NotesTemplate != "{{guestname}}" || backend.backupOptions.Protected != "1" {
		t.Fatalf("backup node/options = %s %#v", backend.backupNode, backend.backupOptions)
	}
	if !task.waited {
		t.Fatal("expected wait")
	}
}

func TestLXCBackupCommandWritesResult(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	task := &commandTask{upid: "UPID:pve1:lxcbackup"}
	backend := &commandBackend{
		lxcGuests:  map[string]map[int]*commandGuest{"pve1": {200: {row: output.GuestRow{Kind: "lxc", VMID: 200, Node: "pve1"}}}},
		backupTask: task,
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"lxc", "backup", "200",
		"--node", "pve1",
		"--storage", "backup",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, `"kind": "lxc"`) || !strings.Contains(got, `"storage": "backup"`) {
		t.Fatalf("stdout = %s", got)
	}
	if backend.backupOptions.Kind != "lxc" || task.waited {
		t.Fatalf("backup options = %#v waited=%v", backend.backupOptions, task.waited)
	}
}

func TestVMBackupCommandRejectsInvalidFlags(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")

	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"vm", "backup", "100",
		"--storage", "backup",
		"--mode", "bad",
	}, "test", testDeps(&bytes.Buffer{}, &commandBackend{}))
	if err == nil || err.Error() != `invalid backup mode "bad", expected snapshot, suspend, or stop` {
		t.Fatalf("mode error = %v", err)
	}

	err = RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"vm", "backup", "100",
		"--storage", "backup",
		"--compress", "bad",
	}, "test", testDeps(&bytes.Buffer{}, &commandBackend{}))
	if err == nil || err.Error() != `invalid backup compression "bad", expected zstd, lzo, gzip, or none` {
		t.Fatalf("compress error = %v", err)
	}

	err = RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"vm", "backup", "100",
		"--storage", "backup",
		"--protected", "2",
	}, "test", testDeps(&bytes.Buffer{}, &commandBackend{}))
	if err == nil || err.Error() != `invalid protected value "2", expected 0 or 1` {
		t.Fatalf("protected error = %v", err)
	}
}
