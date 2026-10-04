package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/lz-wang/pvectl/internal/output"
)

func TestCommandJSONGoldenOutputs(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	backend := goldenBackend()
	build := BuildInfo{
		Version: "v1.0.0",
		Commit:  "abc1234",
		Date:    "2026-06-06T00:00:00Z",
	}

	cases := []struct {
		name       string
		args       []string
		goldenFile string
	}{
		{
			name:       "node ls",
			args:       []string{"pve", "--config", cfgPath, "node", "ls", "-o", "json"},
			goldenFile: "node_ls.json",
		},
		{
			name:       "guest ls",
			args:       []string{"pve", "--config", cfgPath, "guest", "ls", "-o", "json"},
			goldenFile: "guest_ls.json",
		},
		{
			name:       "guest get",
			args:       []string{"pve", "--config", cfgPath, "guest", "get", "100", "-o", "json"},
			goldenFile: "guest_get.json",
		},
		{
			name:       "vm get",
			args:       []string{"pve", "--config", cfgPath, "vm", "get", "100", "-o", "json"},
			goldenFile: "vm_get.json",
		},
		{
			name:       "lxc get",
			args:       []string{"pve", "--config", cfgPath, "lxc", "get", "200", "-o", "json"},
			goldenFile: "lxc_get.json",
		},
		{
			name:       "backup ls",
			args:       []string{"pve", "--config", cfgPath, "backup", "ls", "--node", "pve1", "--storage", "backup", "-o", "json"},
			goldenFile: "backup_ls.json",
		},
		{
			name:       "storage ls",
			args:       []string{"pve", "--config", cfgPath, "storage", "ls", "-o", "json"},
			goldenFile: "storage_ls.json",
		},
		{
			name:       "storage content ls",
			args:       []string{"pve", "--config", cfgPath, "storage", "content", "ls", "--node", "pve1", "--storage", "backup", "-o", "json"},
			goldenFile: "storage_content_ls.json",
		},
		{
			name:       "doctor offline",
			args:       []string{"pve", "--config", cfgPath, "doctor", "--offline", "-o", "json"},
			goldenFile: "doctor_offline.json",
		},
		{
			name:       "version",
			args:       []string{"pve", "version", "-o", "json"},
			goldenFile: "version.json",
		},
		{
			name:       "task ls",
			args:       []string{"pve", "--config", cfgPath, "task", "ls", "-o", "json"},
			goldenFile: "task_ls.json",
		},
		{
			name:       "status",
			args:       []string{"pve", "--config", cfgPath, "status", "-o", "json"},
			goldenFile: "status.json",
		},
		{
			name:       "node get",
			args:       []string{"pve", "--config", cfgPath, "node", "get", "pve1", "-o", "json"},
			goldenFile: "node_get.json",
		},
		{
			name:       "storage usage",
			args:       []string{"pve", "--config", cfgPath, "storage", "usage", "-o", "json"},
			goldenFile: "storage_usage.json",
		},
		{
			name:       "check",
			args:       []string{"pve", "--config", cfgPath, "check", "-o", "json"},
			goldenFile: "check.json",
		},
		{
			name:       "vm restore",
			args:       []string{"pve", "--config", cfgPath, "vm", "restore", "backup:backup/vzdump-qemu-100-2026_06_06-00_00_00.vma.zst", "--node", "pve1", "--vmid", "101", "--storage", "local-lvm", "-o", "json"},
			goldenFile: "vm_restore.json",
		},
		{
			name:       "vm agent network",
			args:       []string{"pve", "--config", cfgPath, "vm", "agent", "network", "100", "-o", "json"},
			goldenFile: "vm_agent_network.json",
		},
		{
			name:       "vm cloud-init get",
			args:       []string{"pve", "--config", cfgPath, "vm", "cloud-init", "get", "100", "-o", "json"},
			goldenFile: "vm_cloudinit_get.json",
		},
		{
			name:       "network ls",
			args:       []string{"pve", "--config", cfgPath, "network", "ls", "-o", "json"},
			goldenFile: "network_ls.json",
		},
		{
			name:       "firewall status",
			args:       []string{"pve", "--config", cfgPath, "firewall", "status", "--node", "pve1", "-o", "json"},
			goldenFile: "firewall_status.json",
		},
		{
			name:       "firewall ls",
			args:       []string{"pve", "--config", cfgPath, "firewall", "ls", "--node", "pve1", "-o", "json"},
			goldenFile: "firewall_ls.json",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := RunWithBuildInfoAndDependencies(tc.args, build, commandDeps(&stdout, &stderr, backend))
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if stderr.String() != "" {
				t.Fatalf("stderr = %s", stderr.String())
			}

			got := stdout.String()
			if os.Getenv("PVE_UPDATE_GOLDEN") == "1" {
				// Restore the placeholders so regenerated goldens stay stable
				// across machines.
				restore := strings.NewReplacer(
					cfgPath, "{{CONFIG_PATH}}",
					goruntime.Version(), "{{GO_VERSION}}",
					goruntime.GOOS, "{{GOOS}}",
					goruntime.GOARCH, "{{GOARCH}}",
				)
				if err := os.WriteFile(filepath.Join("testdata", "golden", tc.goldenFile), []byte(restore.Replace(got)), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
			}
			want := goldenOutput(t, tc.goldenFile, cfgPath)
			if got != want {
				t.Fatalf("stdout mismatch\nwant:\n%s\ngot:\n%s", want, got)
			}
			assertValidJSON(t, got)
		})
	}
}

func TestAsyncJSONKeepsTaskProgressOnStderr(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	task := &commandTask{upid: "UPID:pve1:backup"}
	backend := goldenBackend()
	backend.backupTask = task

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"vm", "backup", "100",
		"--storage", "backup",
		"--wait",
		"-o", "json",
	}, "test", commandDeps(&stdout, &stderr, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	assertValidJSON(t, stdout.String())
	if strings.Contains(stdout.String(), "waiting for task") || strings.Contains(stdout.String(), "task:") {
		t.Fatalf("stdout includes task progress: %s", stdout.String())
	}
	for _, want := range []string{
		"task: UPID:pve1:backup",
		"waiting for task: UPID:pve1:backup",
		"task completed: UPID:pve1:backup",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr missing %q in %s", want, stderr.String())
		}
	}
	if !task.waited {
		t.Fatal("expected task wait")
	}
}

func TestDoctorFailureJSONWritesRowsToStdout(t *testing.T) {
	var stdout bytes.Buffer
	err := RunWithDependencies([]string{
		"pve",
		"--config", filepath.Join(t.TempDir(), "missing.yaml"),
		"doctor", "--offline", "-o", "json",
	}, "test", Dependencies{Stdout: &stdout, Stderr: &bytes.Buffer{}})
	if err == nil || err.Error() != "doctor checks failed" {
		t.Fatalf("err = %v", err)
	}

	var rows []output.DoctorRow
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
		t.Fatalf("doctor stdout is not JSON rows: %v\n%s", err, stdout.String())
	}
	if len(rows) == 0 || rows[0].Check != "CONFIG_PATH" {
		t.Fatalf("doctor rows = %#v", rows)
	}
}

func goldenBackend() *commandBackend {
	vmRow := output.GuestRow{
		Kind: "vm", VMID: 100, Name: "debian", Node: "pve1", Status: "running",
		CPUs: 2, CPU: 0.1, Mem: 536870912, MaxMem: 2147483648, MaxDisk: 10737418240,
		Uptime: 120, Tags: "lab",
	}
	lxcRow := output.GuestRow{
		Kind: "lxc", VMID: 200, Name: "app", Node: "pve1", Status: "stopped",
		CPUs: 1, CPU: 0.02, Mem: 268435456, MaxMem: 1073741824, MaxDisk: 5368709120,
		Tags: "svc",
	}

	return &commandBackend{
		nodes: []output.NodeRow{{
			Name: "pve1", Status: "online", CPU: 0.25, Mem: 1073741824,
			MaxMem: 4294967296, Disk: 2147483648, MaxDisk: 8589934592, Uptime: 3660,
		}},
		nodeDetails: map[string]output.NodeDetail{
			"pve1": {
				Name: "pve1", Status: "online", CPU: 0.25, Mem: 1073741824,
				MaxMem: 4294967296, Disk: 2147483648, MaxDisk: 8589934592, Uptime: 3660,
				PVEVersion: "8.4.1", KernelVersion: "6.8.12-4-pve", LoadAverage: "0.50, 0.40, 0.30",
				CPUModel: "Test CPU", CPUCores: 8, CPUSockets: 1,
			},
		},
		taskRows: map[string][]output.TaskRow{
			"pve1": {{
				UPID: "UPID:pve1:0000F2A3:00000000:6839F4A1:vzdump:100:root@pam:",
				Node: "pve1", Type: "vzdump", ID: "100", User: "root@pam:",
				Status: "ok", ExitStatus: "OK", StartTime: 1710000000, EndTime: 1710000600,
			}},
		},
		agentNetwork: []output.AgentNetworkRow{
			{Name: "eth0", HardwareAddress: "bc:24:11:2f:ab:12", Addresses: []string{"192.168.2.10/24"}},
		},
		cloudInitConfigs: map[int]output.CloudInitConfig{
			100: {
				VMID: 100, Node: "pve1", User: "debian", PasswordConfigured: true,
				SSHKeys:    "ssh-ed25519 AAAAtest key@host",
				IPConfigs:  []output.CloudInitIPConfig{{Device: "ipconfig0", Config: "ip=dhcp"}},
				Nameserver: "192.168.2.67", SearchDomain: "lan", Type: "nocloud",
			},
		},
		networkRows: map[string][]output.NetworkRow{
			"pve1": {{
				Node: "pve1", Name: "vmbr0", Type: "bridge", Active: true, Autostart: true,
				CIDR: "192.168.2.2/24", Gateway: "192.168.2.1", BridgePorts: "enp1s0",
			}},
		},
		firewallStatus: map[string]output.FirewallStatusRow{
			"node": {Scope: "node", Node: "pve1", Enabled: true},
		},
		firewallRules: map[string][]output.FirewallRuleRow{
			"node": {{
				Scope: "node", Node: "pve1", Position: 1, Enabled: true,
				Direction: "in", Action: "ACCEPT", DestPort: "22", Protocol: "tcp",
				Source: "192.168.2.0/24", Comment: "ssh from lan",
			}},
		},
		vms:       map[string][]output.GuestRow{"pve1": {vmRow}},
		lxcs:      map[string][]output.GuestRow{"pve1": {lxcRow}},
		vmGuests:  map[string]map[int]*commandGuest{"pve1": {100: {row: vmRow}}},
		lxcGuests: map[string]map[int]*commandGuest{"pve1": {200: {row: lxcRow}}},
		backups: map[string]map[string][]output.BackupRow{
			"pve1": {
				"backup": {{
					Node: "pve1", Storage: "backup", Kind: "vm", VMID: 100,
					VolID:  "backup:backup/vzdump-qemu-100-2026_06_06-00_00_00.vma.zst",
					Format: "vma.zst", Size: 1024, Used: 512, CTime: 1710000000,
					Protected: "1", Encrypted: "0", VerifyState: "ok", Notes: "daily",
				}},
			},
		},
		storages: map[string][]output.StorageRow{
			"pve1": {{
				Node: "pve1", Storage: "backup", Type: "dir", Active: true, Enabled: true,
				Shared: false, Content: "backup,iso", Used: 1024, Avail: 2048, Total: 3072,
				UsedFraction: 0.3333333333,
			}},
		},
		storageContent: map[string]map[string][]output.StorageContentRow{
			"pve1": {
				"backup": {{
					Node: "pve1", Storage: "backup", Content: "backup", VMID: 100,
					VolID:  "backup:backup/vzdump-qemu-100-2026_06_06-00_00_00.vma.zst",
					Format: "vma.zst", Size: 1024, Used: 512, CTime: 1710000000,
					Protected: "1", Encrypted: "0", VerifyState: "ok", Notes: "daily",
				}},
			},
		},
	}
}

func goldenOutput(t *testing.T, name, cfgPath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "golden", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	replacer := strings.NewReplacer(
		"{{CONFIG_PATH}}", cfgPath,
		"{{GO_VERSION}}", goruntime.Version(),
		"{{GOOS}}", goruntime.GOOS,
		"{{GOARCH}}", goruntime.GOARCH,
	)
	return replacer.Replace(string(data))
}

func assertValidJSON(t *testing.T, value string) {
	t.Helper()
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, value)
	}
}
