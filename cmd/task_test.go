package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/lz-wang/pvectl/internal/output"
	"github.com/lz-wang/pvectl/internal/pve"
)

const testUPID = "UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam"

func TestNodeGetCommandWritesDetail(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	backend := &commandBackend{
		nodeDetails: map[string]output.NodeDetail{
			"pve1": {
				Name: "pve1", Status: "online", CPU: 0.12, Mem: 100, MaxMem: 200,
				PVEVersion: "8.4.1", KernelVersion: "6.8.12-4-pve", CPUModel: "M4", CPUCores: 12, CPUSockets: 1,
			},
		},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"node", "get", "pve1",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{`"pve_version": "8.4.1"`, `"kernel_version": "6.8.12-4-pve"`, `"cpu_cores": 12`} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %s: %s", want, out)
		}
	}
}

func TestStorageUsageCommandWritesUsage(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		storages: map[string][]output.StorageRow{
			"pve1": {
				{Node: "pve1", Storage: "local", Type: "dir", Active: true, Used: 35 * 1024 * 1024 * 1024, Avail: 57 * 1024 * 1024 * 1024, Total: 92 * 1024 * 1024 * 1024, UsedFraction: 0.38},
			},
		},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"storage", "usage",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, `"storage": "local"`) || !strings.Contains(out, `"used_fraction": 0.38`) {
		t.Fatalf("stdout = %s", out)
	}
}

func TestStatusCommandWritesReport(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1", Status: "online", CPU: 0.12, Mem: 100, MaxMem: 200, Uptime: 3600}},
		vms: map[string][]output.GuestRow{
			"pve1": {{Kind: "vm", VMID: 100, Node: "pve1", Status: "running"}},
		},
		lxcs: map[string][]output.GuestRow{
			"pve1": {{Kind: "lxc", VMID: 200, Node: "pve1", Status: "stopped"}},
		},
		storages: map[string][]output.StorageRow{
			"pve1": {
				{Node: "pve1", Storage: "local", Active: true, Content: "iso"},
				{Node: "pve1", Storage: "backup", Active: true, Content: "backup"},
			},
		},
		backups: map[string]map[string][]output.BackupRow{
			"pve1": {"backup": {{Node: "pve1", Storage: "backup", Kind: "vm", VMID: 100, CTime: 1710000000}}},
		},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"status",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{`"total": 2`, `"running": 1`, `"stopped": 1`, `"count": 1`, `"latest_ctime": 1710000000`} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %s: %s", want, out)
		}
	}
}

func TestStatusCommandReportsPartialIssues(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	backend := &commandBackend{
		nodes:     []output.NodeRow{{Name: "pve1", Status: "online"}},
		storageErrs: map[string]error{"pve1": errors.New("timeout")},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"status",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), `"component": "storage"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestTaskListCommandWritesRows(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		taskRows: map[string][]output.TaskRow{
			"pve1": {
				{UPID: testUPID, Node: "pve1", Type: "vzdump", Status: "ok", StartTime: 1710000000, EndTime: 1710000600},
				{UPID: "UPID:pve1:0002:0000:6839F4A2:qmstart:101:root@pam", Node: "pve1", Type: "qmstart", Status: "running", StartTime: 1710000700},
			},
		},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"task", "ls",
		"--type", "vzdump",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), `"upid": "UPID:pve1:0001`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "qmstart") {
		t.Fatalf("expected type filter to drop qmstart: %s", stdout.String())
	}
}

func TestTaskListCommandPartialNodeFailure(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	backend := &commandBackend{
		nodes:    []output.NodeRow{{Name: "pve1"}, {Name: "pve2"}},
		taskErrs: map[string]error{"pve1": errors.New("forbidden")},
		taskRows: map[string][]output.TaskRow{
			"pve2": {{UPID: "UPID:pve2:0002:0000:6839F4A2:qmstart:101:root@pam", Node: "pve2", Type: "qmstart", Status: "running"}},
		},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"task", "ls",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), `"node": "pve2"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestTaskGetCommandWritesDetail(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		taskByName: map[string]output.TaskRow{
			testUPID: {UPID: testUPID, Node: "pve1", Type: "vzdump", Status: "ok", ExitStatus: "OK"},
		},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"task", "get", testUPID,
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), `"exit_status": "OK"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestTaskLogCommandWritesRowsWithTail(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		taskLog: map[string][]output.TaskLogRow{
			testUPID: {
				{Line: 1, Text: "INFO: starting new backup job"},
				{Line: 2, Text: "INFO: backup finished"},
			},
		},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"task", "log", testUPID,
		"--tail", "1",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "backup finished") || strings.Contains(out, "starting new backup job") {
		t.Fatalf("stdout = %s", out)
	}
}

func TestTaskWaitCommandWritesFinalRow(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		taskHandles: map[string]pve.Task{
			testUPID: &commandTask{upid: testUPID},
		},
		taskByName: map[string]output.TaskRow{
			testUPID: {UPID: testUPID, Node: "pve1", Type: "vzdump", Status: "ok", ExitStatus: "OK"},
		},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"task", "wait", testUPID,
		"--wait-timeout", "1s",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), `"status": "ok"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}
