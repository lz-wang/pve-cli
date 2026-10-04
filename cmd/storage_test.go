package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lz-wang/pvectl/internal/output"
)

func TestStorageListCommandWritesJSONAndFilters(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}, {Name: "pve2"}},
		storages: map[string][]output.StorageRow{
			"pve1": {
				{Node: "pve1", Storage: "local", Type: "dir", Active: true, Enabled: true, Content: "iso,backup"},
				{Node: "pve1", Storage: "images", Type: "lvmthin", Active: true, Enabled: true, Content: "images"},
			},
			"pve2": {
				{Node: "pve2", Storage: "offline", Type: "dir", Active: false, Enabled: true, Content: "backup"},
			},
		},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"storage", "ls",
		"--content", "backup",
		"--type", "dir",
		"--active",
		"-o", "json",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, `"storage": "local"`) || strings.Contains(got, "images") || strings.Contains(got, "offline") {
		t.Fatalf("stdout = %s", got)
	}
}

func TestStorageGetCommandWritesDetail(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	backend := &commandBackend{
		storageByName: map[string]map[string]output.StorageRow{
			"pve1": {
				"local": {Node: "pve1", Storage: "local", Type: "dir", Active: true, Enabled: true, Content: "iso,vztmpl", Used: 1024, Total: 2048, UsedFraction: 0.5},
			},
		},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"storage", "get", "local",
		"--node", "pve1",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "Storage:") || !strings.Contains(got, "local") || !strings.Contains(got, "Used%") {
		t.Fatalf("stdout = %s", got)
	}
}

func TestStorageContentListCommandWritesJSONAndFilters(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	backend := &commandBackend{
		storageContent: map[string]map[string][]output.StorageContentRow{
			"pve1": {
				"backup": {
					{Node: "pve1", Storage: "backup", Content: "iso", VolID: "backup:iso/debian.iso"},
					{Node: "pve1", Storage: "backup", Content: "backup", VMID: 100, CTime: 10, VolID: "backup:backup/vzdump-qemu-100-old.vma.zst"},
					{Node: "pve1", Storage: "backup", Content: "backup", VMID: 100, CTime: 20, VolID: "backup:backup/vzdump-qemu-100-new.vma.zst"},
					{Node: "pve1", Storage: "backup", Content: "backup", VMID: 200, CTime: 30, VolID: "backup:backup/vzdump-lxc-200.tar.zst"},
				},
			},
		},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"storage", "content", "ls",
		"--node", "pve1",
		"--storage", "backup",
		"--content", "backup",
		"--vmid", "100",
		"-o", "json",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "new") || !strings.Contains(got, "old") || strings.Contains(got, "debian.iso") || strings.Contains(got, `"vmid": 200`) {
		t.Fatalf("stdout = %s", got)
	}
}

func TestStorageCommandsRejectMissingRequiredFlags(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")

	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"storage", "get", "local",
	}, "test", testDeps(&bytes.Buffer{}, &commandBackend{}))
	if err == nil || !strings.Contains(err.Error(), `Required flag "node" not set`) {
		t.Fatalf("get error = %v", err)
	}

	err = RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"storage", "content", "ls",
		"--node", "pve1",
	}, "test", testDeps(&bytes.Buffer{}, &commandBackend{}))
	if err == nil || !strings.Contains(err.Error(), `Required flag "storage" not set`) {
		t.Fatalf("content error = %v", err)
	}
}
