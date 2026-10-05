package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

func TestCheckCommandHealthyExitsZero(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1", Status: "online"}},
		storages: map[string][]output.StorageRow{
			"pve1": {{Node: "pve1", Storage: "local", Active: true, Enabled: true, UsedFraction: 0.38}},
		},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"check",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), "ok") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestCheckCommandFailsOnFailureRows(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1", Status: "offline"}},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"check",
	}, "test", testDeps(&stdout, backend))
	if err == nil {
		t.Fatal("expected failure exit")
	}
	if !strings.Contains(stdout.String(), "fail") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestCheckCommandWarnPassesByDefaultFailsInStrict(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1", Status: "online"}},
		storages: map[string][]output.StorageRow{
			"pve1": {{Node: "pve1", Storage: "backup", Active: true, Enabled: true, UsedFraction: 0.87}},
		},
	}

	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"check",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err != nil {
		t.Fatalf("warnings must pass by default: %v", err)
	}

	err = RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"check",
		"--strict",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err == nil {
		t.Fatal("expected strict mode to fail on warnings")
	}
}

func TestCheckCommandBackupCoverage(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1", Status: "online"}},
		vms: map[string][]output.GuestRow{
			"pve1": {{Kind: "vm", VMID: 100, Node: "pve1", Tags: "backup"}},
		},
		storages: map[string][]output.StorageRow{
			"pve1": {{Node: "pve1", Storage: "backup", Active: true, Enabled: true, Content: "backup"}},
		},
		backups: map[string]map[string][]output.BackupRow{
			"pve1": {"backup": {{Node: "pve1", Storage: "backup", Kind: "vm", VMID: 100, CTime: uint64(time.Now().Add(-time.Hour).Unix())}}},
		},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"check",
		"--backup-tag", "backup",
		"--backup-max-age", "36h",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), "backup-coverage") {
		t.Fatalf("stdout = %s", stdout.String())
	}

	err = RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"check",
		"--backup-tag", "backup",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err == nil {
		t.Fatal("expected backup-tag without backup-max-age to fail")
	}
}

func TestCheckCommandRejectsBadThresholds(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1", Status: "online"}},
	}

	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"check",
		"--storage-warn", "95",
		"--storage-fail", "85",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err == nil {
		t.Fatal("expected warn>=fail threshold error")
	}
}
