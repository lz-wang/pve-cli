package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lz-wang/pvectl/internal/config"
	"github.com/lz-wang/pvectl/internal/output"
	"github.com/lz-wang/pvectl/internal/pve"
)

func TestParseVMID(t *testing.T) {
	if vmid, err := parseVMID("100"); err != nil || vmid != 100 {
		t.Fatalf("parse valid = %d/%v", vmid, err)
	}
	for _, value := range []string{"", "abc", "0", "-1"} {
		if _, err := parseVMID(value); err == nil {
			t.Fatalf("expected invalid vmid for %q", value)
		}
	}
}

func TestParseSetFlags(t *testing.T) {
	values, err := parseSetFlags([]string{"memory=4096", "cores=4"})
	if err != nil {
		t.Fatalf("parse set: %v", err)
	}
	if values["memory"] != "4096" || values["cores"] != "4" {
		t.Fatalf("values = %#v", values)
	}

	for _, values := range [][]string{nil, []string{"memory"}, []string{"=4096"}} {
		if _, err := parseSetFlags(values); err == nil {
			t.Fatalf("expected error for %#v", values)
		}
	}
}

func TestConfirmDelete(t *testing.T) {
	var stderr bytes.Buffer
	if err := confirmDelete(strings.NewReader("101\n"), &stderr, "vm", 101, "pve1"); err != nil {
		t.Fatalf("confirm delete: %v", err)
	}
	if !strings.Contains(stderr.String(), "delete vm 101 on node pve1") {
		t.Fatalf("prompt = %q", stderr.String())
	}

	if err := confirmDelete(strings.NewReader("102\n"), &bytes.Buffer{}, "vm", 101, "pve1"); err == nil {
		t.Fatal("expected mismatch to abort")
	}
	if err := confirmDelete(strings.NewReader(""), &bytes.Buffer{}, "vm", 101, "pve1"); err == nil {
		t.Fatal("expected EOF to abort")
	}
}

func TestConfirmRollback(t *testing.T) {
	var stderr bytes.Buffer
	if err := confirmRollback(strings.NewReader("before-upgrade\n"), &stderr, "vm", 101, "pve1", "before-upgrade"); err != nil {
		t.Fatalf("confirm rollback: %v", err)
	}
	if !strings.Contains(stderr.String(), "rollback vm 101 on node pve1 to snapshot before-upgrade") {
		t.Fatalf("prompt = %q", stderr.String())
	}

	if err := confirmRollback(strings.NewReader("other\n"), &bytes.Buffer{}, "vm", 101, "pve1", "before-upgrade"); err == nil {
		t.Fatal("expected mismatch to abort")
	}
	if err := confirmRollback(strings.NewReader(""), &bytes.Buffer{}, "vm", 101, "pve1", "before-upgrade"); err == nil {
		t.Fatal("expected EOF to abort")
	}
}

func TestVMListCommandUsesDefaultOutputFromProfile(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")

	var stdout bytes.Buffer
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string][]output.GuestRow{"pve1": {{Kind: "vm", VMID: 100, Name: "debian", Node: "pve1"}}},
	}
	deps := Dependencies{
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
		BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) {
			return backend, nil
		},
	}

	err := RunWithDependencies([]string{"pvectl", "--config", cfgPath, "vm", "ls"}, "test", deps)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), `"vmid": 100`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestVMListCommandUsesSelectedProfile(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("PVECTL_TOKEN", "secret")
	cfg := config.Empty()
	if err := cfg.SetProfile("home", config.Profile{
		Endpoint:       "https://pve-home.example:8006/api2/json",
		TokenID:        "root@pam!home",
		TokenSecretEnv: "PVECTL_TOKEN",
		DefaultOutput:  "table",
	}); err != nil {
		t.Fatalf("set home profile: %v", err)
	}
	if err := cfg.SetProfile("lab", config.Profile{
		Endpoint:       "https://pve-lab.example:8006/api2/json",
		TokenID:        "root@pam!lab",
		TokenSecretEnv: "PVECTL_TOKEN",
		DefaultOutput:  "json",
	}); err != nil {
		t.Fatalf("set lab profile: %v", err)
	}
	if err := cfg.UseProfile("home"); err != nil {
		t.Fatalf("use home profile: %v", err)
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	var stdout bytes.Buffer
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string][]output.GuestRow{"pve1": {{Kind: "vm", VMID: 100, Name: "debian", Node: "pve1"}}},
	}
	var selected config.Profile
	deps := Dependencies{
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
		BackendFactory: func(profile config.Profile, _ pve.ClientOptions) (pve.Backend, error) {
			selected = profile
			return backend, nil
		},
	}

	err := RunWithDependencies([]string{"pvectl", "--config", cfgPath, "--profile", "lab", "vm", "ls"}, "test", deps)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if selected.TokenID != "root@pam!lab" {
		t.Fatalf("selected profile = %#v", selected)
	}
	if !strings.Contains(stdout.String(), `"vmid": 100`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestVMCloneCommandPrintsNewVMIDAndWaits(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	task := &commandTask{upid: "UPID:pve1:clone"}
	guest := &commandGuest{
		row:     output.GuestRow{Kind: "vm", VMID: 9000, Node: "pve1"},
		task:    task,
		cloneID: 101,
	}
	backend := &commandBackend{
		nodes:    []output.NodeRow{{Name: "pve1"}},
		vmGuests: map[string]map[int]*commandGuest{"pve1": {9000: guest}},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"vm", "clone", "9000",
		"--newid", "101",
		"--name", "app-vm",
		"--target", "pve2",
		"--wait",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), `"new_vmid": 101`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !task.waited {
		t.Fatal("expected clone task to be waited")
	}
	if guest.cloneOptions.Name != "app-vm" || guest.cloneOptions.Target != "pve2" || guest.cloneOptions.NewID != 101 {
		t.Fatalf("clone options = %#v", guest.cloneOptions)
	}
}

func TestVMRebootCommandWaits(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	task := &commandTask{upid: "UPID:pve1:reboot"}
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &commandBackend{
		nodes:    []output.NodeRow{{Name: "pve1"}},
		vmGuests: map[string]map[int]*commandGuest{"pve1": {101: guest}},
	}

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"vm", "reboot", "101",
		"--wait",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !guest.rebooted {
		t.Fatal("expected reboot")
	}
	if !task.waited {
		t.Fatal("expected wait")
	}
}

func TestLXCRebootCommandWaits(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	task := &commandTask{upid: "UPID:pve1:lxcreboot"}
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve1"},
		task: task,
	}
	backend := &commandBackend{
		nodes:     []output.NodeRow{{Name: "pve1"}},
		lxcGuests: map[string]map[int]*commandGuest{"pve1": {201: guest}},
	}

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"lxc", "reboot", "201",
		"--wait",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !guest.rebooted {
		t.Fatal("expected reboot")
	}
	if !task.waited {
		t.Fatal("expected wait")
	}
}

func TestLXCCloneCommandMapsHostname(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	task := &commandTask{upid: "UPID:pve1:lxcclone"}
	guest := &commandGuest{
		row:     output.GuestRow{Kind: "lxc", VMID: 900, Node: "pve1"},
		task:    task,
		cloneID: 201,
	}
	backend := &commandBackend{
		nodes:     []output.NodeRow{{Name: "pve1"}},
		lxcGuests: map[string]map[int]*commandGuest{"pve1": {900: guest}},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"lxc", "clone", "900",
		"--newid", "201",
		"--hostname", "app-lxc",
		"--target", "pve1",
		"--wait",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), `"new_vmid": 201`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !task.waited {
		t.Fatal("expected clone task to be waited")
	}
	if guest.cloneOptions.Hostname != "app-lxc" || guest.cloneOptions.Target != "pve1" {
		t.Fatalf("clone options = %#v", guest.cloneOptions)
	}
}

func TestVMConfigCommandPassesSetValuesAndWaits(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	task := &commandTask{upid: "UPID:pve1:config"}
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &commandBackend{
		nodes:    []output.NodeRow{{Name: "pve1"}},
		vmGuests: map[string]map[int]*commandGuest{"pve1": {101: guest}},
	}

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"vm", "config", "101",
		"--set", "memory=4096",
		"--set", "cores=4",
		"--wait",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !task.waited {
		t.Fatal("expected config task to be waited")
	}
	if guest.configValues["memory"] != "4096" || guest.configValues["cores"] != "4" {
		t.Fatalf("config values = %#v", guest.configValues)
	}
}

func TestLXCConfigCommandPassesSetValues(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	task := &commandTask{upid: "UPID:pve1:lxcconfig"}
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve1"},
		task: task,
	}
	backend := &commandBackend{
		nodes:     []output.NodeRow{{Name: "pve1"}},
		lxcGuests: map[string]map[int]*commandGuest{"pve1": {201: guest}},
	}

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"lxc", "config", "201",
		"--set", "memory=2048",
		"--set", "cores=2",
		"--wait",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !task.waited {
		t.Fatal("expected config task to be waited")
	}
	if guest.configValues["memory"] != "2048" || guest.configValues["cores"] != "2" {
		t.Fatalf("config values = %#v", guest.configValues)
	}
}

func TestVMDeleteCommandForceSkipsConfirmation(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	task := &commandTask{upid: "UPID:pve1:delete"}
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &commandBackend{
		nodes:    []output.NodeRow{{Name: "pve1"}},
		vmGuests: map[string]map[int]*commandGuest{"pve1": {101: guest}},
	}

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"vm", "delete", "101",
		"--force",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !guest.deleted {
		t.Fatal("expected delete")
	}
	if task.waited {
		t.Fatal("did not expect wait without --wait")
	}
}

func TestLXCDeleteCommandConfirmsAndWaits(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	task := &commandTask{upid: "UPID:pve1:lxcdelete"}
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve1"},
		task: task,
	}
	backend := &commandBackend{
		nodes:     []output.NodeRow{{Name: "pve1"}},
		lxcGuests: map[string]map[int]*commandGuest{"pve1": {201: guest}},
	}
	deps := testDeps(&bytes.Buffer{}, backend)
	deps.Stdin = strings.NewReader("201\n")

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"lxc", "delete", "201",
		"--wait",
	}, "test", deps)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !guest.deleted {
		t.Fatal("expected delete")
	}
	if !task.waited {
		t.Fatal("expected wait")
	}
}

func TestDeleteCommandAbortsOnConfirmationMismatch(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: &commandTask{upid: "UPID:pve1:delete"},
	}
	backend := &commandBackend{
		nodes:    []output.NodeRow{{Name: "pve1"}},
		vmGuests: map[string]map[int]*commandGuest{"pve1": {101: guest}},
	}
	deps := testDeps(&bytes.Buffer{}, backend)
	deps.Stdin = strings.NewReader("102\n")

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"vm", "delete", "101",
	}, "test", deps)
	if err == nil {
		t.Fatal("expected abort error")
	}
	if guest.deleted {
		t.Fatal("delete should not run after confirmation mismatch")
	}
}

func TestVMMigrateCommandPassesTargetOnlineAndWaits(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	task := &commandTask{upid: "UPID:pve1:migrate"}
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &commandBackend{
		nodes:    []output.NodeRow{{Name: "pve1"}},
		vmGuests: map[string]map[int]*commandGuest{"pve1": {101: guest}},
	}

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"vm", "migrate", "101",
		"--target", "pve2",
		"--online",
		"--wait",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if guest.migrateOptions.Target != "pve2" || !guest.migrateOptions.Online {
		t.Fatalf("migrate options = %#v", guest.migrateOptions)
	}
	if !task.waited {
		t.Fatal("expected wait")
	}
}

func TestLXCMigrateCommandPassesTargetOnline(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve1"},
		task: &commandTask{upid: "UPID:pve1:lxcmigrate"},
	}
	backend := &commandBackend{
		nodes:     []output.NodeRow{{Name: "pve1"}},
		lxcGuests: map[string]map[int]*commandGuest{"pve1": {201: guest}},
	}

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"lxc", "migrate", "201",
		"--target", "pve2",
		"--online",
		"--wait",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if guest.migrateOptions.Target != "pve2" || !guest.migrateOptions.Online {
		t.Fatalf("migrate options = %#v", guest.migrateOptions)
	}
}

func TestVMResizeCommandPassesDiskSizeAndWaits(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	task := &commandTask{upid: "UPID:pve1:resize"}
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &commandBackend{
		nodes:    []output.NodeRow{{Name: "pve1"}},
		vmGuests: map[string]map[int]*commandGuest{"pve1": {101: guest}},
	}

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"vm", "resize", "101",
		"--disk", "scsi0",
		"--size", "+20G",
		"--wait",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if guest.resizeDisk != "scsi0" || guest.resizeSize != "+20G" {
		t.Fatalf("resize = %s/%s", guest.resizeDisk, guest.resizeSize)
	}
	if !task.waited {
		t.Fatal("expected wait")
	}
}

func TestLXCResizeCommandPassesDiskSize(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve1"},
		task: &commandTask{upid: "UPID:pve1:lxcresize"},
	}
	backend := &commandBackend{
		nodes:     []output.NodeRow{{Name: "pve1"}},
		lxcGuests: map[string]map[int]*commandGuest{"pve1": {201: guest}},
	}

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"lxc", "resize", "201",
		"--disk", "rootfs",
		"--size", "+10G",
		"--wait",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if guest.resizeDisk != "rootfs" || guest.resizeSize != "+10G" {
		t.Fatalf("resize = %s/%s", guest.resizeDisk, guest.resizeSize)
	}
}

func TestVMSnapshotListCommandWritesRows(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	guest := &commandGuest{
		row: output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		snapshots: []output.SnapshotRow{
			{Kind: "vm", VMID: 101, Node: "pve1", Name: "before-upgrade"},
		},
	}
	backend := &commandBackend{
		nodes:    []output.NodeRow{{Name: "pve1"}},
		vmGuests: map[string]map[int]*commandGuest{"pve1": {101: guest}},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"vm", "snapshot", "ls", "101",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), `"name": "before-upgrade"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestLXCSnapshotListCommandWritesRows(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	guest := &commandGuest{
		row: output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve1"},
		snapshots: []output.SnapshotRow{
			{Kind: "lxc", VMID: 201, Node: "pve1", Name: "before-upgrade"},
		},
	}
	backend := &commandBackend{
		nodes:     []output.NodeRow{{Name: "pve1"}},
		lxcGuests: map[string]map[int]*commandGuest{"pve1": {201: guest}},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"lxc", "snapshot", "ls", "201",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), `"name": "before-upgrade"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestVMSnapshotCreateCommandWaits(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	task := &commandTask{upid: "UPID:pve1:snapshot"}
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &commandBackend{
		nodes:    []output.NodeRow{{Name: "pve1"}},
		vmGuests: map[string]map[int]*commandGuest{"pve1": {101: guest}},
	}

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"vm", "snapshot", "create", "101", "before-upgrade",
		"--wait",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if guest.createdSnapshot != "before-upgrade" {
		t.Fatalf("created snapshot = %q", guest.createdSnapshot)
	}
	if !task.waited {
		t.Fatal("expected wait")
	}
}

func TestLXCSnapshotCreateCommandWaits(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	task := &commandTask{upid: "UPID:pve1:lxcsnapshot"}
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve1"},
		task: task,
	}
	backend := &commandBackend{
		nodes:     []output.NodeRow{{Name: "pve1"}},
		lxcGuests: map[string]map[int]*commandGuest{"pve1": {201: guest}},
	}

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"lxc", "snapshot", "create", "201", "before-upgrade",
		"--wait",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if guest.createdSnapshot != "before-upgrade" {
		t.Fatalf("created snapshot = %q", guest.createdSnapshot)
	}
	if !task.waited {
		t.Fatal("expected wait")
	}
}

func TestVMSnapshotRollbackCommandForceWaits(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	task := &commandTask{upid: "UPID:pve1:rollback"}
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &commandBackend{
		nodes:    []output.NodeRow{{Name: "pve1"}},
		vmGuests: map[string]map[int]*commandGuest{"pve1": {101: guest}},
	}

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"vm", "snapshot", "rollback", "101", "before-upgrade",
		"--force",
		"--wait",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if guest.rollbackSnapshot != "before-upgrade" {
		t.Fatalf("rollback snapshot = %q", guest.rollbackSnapshot)
	}
	if !task.waited {
		t.Fatal("expected wait")
	}
}

func TestLXCSnapshotRollbackCommandForceWaits(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	task := &commandTask{upid: "UPID:pve1:lxcrollback"}
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve1"},
		task: task,
	}
	backend := &commandBackend{
		nodes:     []output.NodeRow{{Name: "pve1"}},
		lxcGuests: map[string]map[int]*commandGuest{"pve1": {201: guest}},
	}

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"lxc", "snapshot", "rollback", "201", "before-upgrade",
		"--force",
		"--wait",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if guest.rollbackSnapshot != "before-upgrade" {
		t.Fatalf("rollback snapshot = %q", guest.rollbackSnapshot)
	}
	if !task.waited {
		t.Fatal("expected wait")
	}
}

func TestSnapshotRollbackCommandConfirms(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: &commandTask{upid: "UPID:pve1:rollback"},
	}
	backend := &commandBackend{
		nodes:    []output.NodeRow{{Name: "pve1"}},
		vmGuests: map[string]map[int]*commandGuest{"pve1": {101: guest}},
	}
	deps := testDeps(&bytes.Buffer{}, backend)
	deps.Stdin = strings.NewReader("before-upgrade\n")

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"vm", "snapshot", "rollback", "101", "before-upgrade",
	}, "test", deps)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if guest.rollbackSnapshot != "before-upgrade" {
		t.Fatalf("rollback snapshot = %q", guest.rollbackSnapshot)
	}
}

func TestVMSnapshotDeleteCommandForceWaits(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	task := &commandTask{upid: "UPID:pve1:snapdelete"}
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"},
		task: task,
	}
	backend := &commandBackend{
		nodes:    []output.NodeRow{{Name: "pve1"}},
		vmGuests: map[string]map[int]*commandGuest{"pve1": {101: guest}},
	}

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"vm", "snapshot", "delete", "101", "before-upgrade",
		"--force",
		"--wait",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if guest.deletedSnapshot != "before-upgrade" {
		t.Fatalf("deleted snapshot = %q", guest.deletedSnapshot)
	}
	if !task.waited {
		t.Fatal("expected wait")
	}
}

func TestSnapshotDeleteCommandConfirms(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	guest := &commandGuest{
		row:  output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve1"},
		task: &commandTask{upid: "UPID:pve1:lxcsnapdelete"},
	}
	backend := &commandBackend{
		nodes:     []output.NodeRow{{Name: "pve1"}},
		lxcGuests: map[string]map[int]*commandGuest{"pve1": {201: guest}},
	}
	deps := testDeps(&bytes.Buffer{}, backend)
	deps.Stdin = strings.NewReader("wrong-name\n")

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"lxc", "snapshot", "delete", "201", "before-upgrade",
	}, "test", deps)
	if err == nil {
		t.Fatal("expected confirmation mismatch to abort")
	}
	if guest.deletedSnapshot != "" {
		t.Fatal("snapshot should not be deleted after confirmation mismatch")
	}
}

func TestVMRestoreCommandWritesResultAndWaits(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	task := &commandTask{upid: "UPID:pve1:restore"}
	backend := &commandBackend{
		nodes:      []output.NodeRow{{Name: "pve1"}},
		backupTask: task,
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"vm", "restore", "backup:backup/vzdump-qemu-100.vma.zst",
		"--node", "pve1",
		"--vmid", "101",
		"--storage", "local-lvm",
		"--wait",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), `"vmid": 101`) || !strings.Contains(stdout.String(), `"task": "UPID:pve1:restore"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !task.waited {
		t.Fatal("expected restore task to be waited")
	}
	if backend.restoreOptions.VMID != 101 || backend.restoreOptions.Storage != "local-lvm" {
		t.Fatalf("restore options = %#v", backend.restoreOptions)
	}
}

func TestRestoreCommandRejectsExistingVMID(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string][]output.GuestRow{"pve1": {{Kind: "vm", VMID: 101, Node: "pve1"}}},
	}

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"vm", "restore", "backup:backup/vzdump-qemu-100.vma.zst",
		"--node", "pve1",
		"--vmid", "101",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err == nil {
		t.Fatal("expected existing vmid error")
	}
	if strings.Contains(err.Error(), "already exists") == false {
		t.Fatalf("error = %v", err)
	}
}

func TestConfigSetProfileCommandDoesNotRequireSecretEnv(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	err := RunWithDependencies([]string{
		"pvectl",
		"--config", cfgPath,
		"config", "set-profile", "home",
		"--endpoint", "https://pve.example:8006/api2/json",
		"--token-id", "root@pam!test",
		"--token-secret-env", "PVECTL_TOKEN",
		"--default-output", "yaml",
	}, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(data)
	if strings.Contains(got, "token_secret:") || !strings.Contains(got, "token_secret_env: PVECTL_TOKEN") {
		t.Fatalf("config content = %s", got)
	}
}

type commandBackend struct {
	nodes                []output.NodeRow
	nodeDetails          map[string]output.NodeDetail
	vms                  map[string][]output.GuestRow
	lxcs                 map[string][]output.GuestRow
	vmGuests             map[string]map[int]*commandGuest
	lxcGuests            map[string]map[int]*commandGuest
	backups              map[string]map[string][]output.BackupRow
	storages             map[string][]output.StorageRow
	storageByName        map[string]map[string]output.StorageRow
	storageContent       map[string]map[string][]output.StorageContentRow
	taskRows             map[string][]output.TaskRow
	taskErrs             map[string]error
	taskByName           map[string]output.TaskRow
	taskLog              map[string][]output.TaskLogRow
	taskHandles          map[string]pve.Task
	restoreOptions       pve.RestoreOptions
	agentErr             error
	agentNetwork         []output.AgentNetworkRow
	agentExecOptions     pve.AgentExecOptions
	agentExecResult      output.AgentExecResult
	cloudInitConfigs     map[int]output.CloudInitConfig
	cloudInitRegenerated int
	networkRows          map[string][]output.NetworkRow
	networkErrs          map[string]error
	firewallStatus       map[string]output.FirewallStatusRow
	firewallRules        map[string][]output.FirewallRuleRow
	backupTask           pve.Task
	backupNode           string
	backupOptions        pve.BackupOptions
	backupCalls          int
	storageErrs          map[string]error
	backupErrs           map[string]error
}

func (b *commandBackend) Nodes(context.Context) ([]output.NodeRow, error) {
	return b.nodes, nil
}

func (b *commandBackend) Node(_ context.Context, name string) (output.NodeDetail, error) {
	if detail, ok := b.nodeDetails[name]; ok {
		return detail, nil
	}
	return output.NodeDetail{}, pve.ErrNotFound
}

func (b *commandBackend) VMs(_ context.Context, node string) ([]output.GuestRow, error) {
	return b.vms[node], nil
}

func (b *commandBackend) VM(_ context.Context, node string, vmid int) (pve.Guest, error) {
	if guest := b.vmGuests[node][vmid]; guest != nil {
		return guest, nil
	}
	return nil, pve.ErrNotFound
}

func (b *commandBackend) LXCs(_ context.Context, node string) ([]output.GuestRow, error) {
	return b.lxcs[node], nil
}

func (b *commandBackend) LXC(_ context.Context, node string, vmid int) (pve.Guest, error) {
	if guest := b.lxcGuests[node][vmid]; guest != nil {
		return guest, nil
	}
	return nil, pve.ErrNotFound
}

func (b *commandBackend) Backups(_ context.Context, node, storage string) ([]output.BackupRow, error) {
	if err := b.backupErrs[node+"/"+storage]; err != nil {
		return nil, err
	}
	if b.backups == nil {
		return nil, nil
	}
	return b.backups[node][storage], nil
}

func (b *commandBackend) BackupGuest(_ context.Context, node string, options pve.BackupOptions) (pve.Task, error) {
	b.backupCalls++
	b.backupNode = node
	b.backupOptions = options
	return b.backupTask, nil
}

func (b *commandBackend) Storages(_ context.Context, node string) ([]output.StorageRow, error) {
	if err := b.storageErrs[node]; err != nil {
		return nil, err
	}
	if b.storages == nil {
		return nil, nil
	}
	return b.storages[node], nil
}

func (b *commandBackend) Storage(_ context.Context, node, storage string) (output.StorageRow, error) {
	if b.storageByName == nil {
		return output.StorageRow{}, pve.ErrNotFound
	}
	row, ok := b.storageByName[node][storage]
	if !ok {
		return output.StorageRow{}, pve.ErrNotFound
	}
	return row, nil
}

func (b *commandBackend) StorageContents(_ context.Context, node, storage string) ([]output.StorageContentRow, error) {
	if b.storageContent == nil {
		return nil, nil
	}
	return b.storageContent[node][storage], nil
}

func (b *commandBackend) Tasks(_ context.Context, node string, _ pve.TaskListBackendOptions) ([]output.TaskRow, error) {
	if err := b.taskErrs[node]; err != nil {
		return nil, err
	}
	return b.taskRows[node], nil
}

func (b *commandBackend) Task(_ context.Context, node, upid string) (output.TaskRow, error) {
	if row, ok := b.taskByName[upid]; ok {
		return row, nil
	}
	return output.TaskRow{}, pve.ErrNotFound
}

func (b *commandBackend) TaskLog(_ context.Context, node, upid string, page pve.TaskLogPage) ([]output.TaskLogRow, error) {
	if rows, ok := b.taskLog[upid]; ok {
		start := page.Start
		if start > len(rows) {
			start = len(rows)
		}
		end := start + page.Limit
		if page.Limit <= 0 || end > len(rows) {
			end = len(rows)
		}
		return rows[start:end], nil
	}
	return nil, nil
}

func (b *commandBackend) TaskHandle(upid string) (pve.Task, error) {
	if task, ok := b.taskHandles[upid]; ok {
		return task, nil
	}
	return nil, pve.ErrNotFound
}

func (b *commandBackend) Restore(_ context.Context, options pve.RestoreOptions) (pve.Task, error) {
	b.restoreOptions = options
	return b.backupTask, nil
}

func (b *commandBackend) AgentPing(_ context.Context, node string, vmid int) error {
	return b.agentErr
}

func (b *commandBackend) AgentNetwork(_ context.Context, node string, vmid int) ([]output.AgentNetworkRow, error) {
	if b.agentErr != nil {
		return nil, b.agentErr
	}
	return b.agentNetwork, nil
}

func (b *commandBackend) AgentExec(_ context.Context, node string, vmid int, options pve.AgentExecOptions) (output.AgentExecResult, error) {
	b.agentExecOptions = options
	if b.agentErr != nil {
		return output.AgentExecResult{}, b.agentErr
	}
	return b.agentExecResult, nil
}

func (b *commandBackend) VirtualMachineCloudInit(_ context.Context, node string, vmid int) (output.CloudInitConfig, error) {
	if detail, ok := b.cloudInitConfigs[vmid]; ok {
		return detail, nil
	}
	return output.CloudInitConfig{}, pve.ErrNotFound
}

func (b *commandBackend) RegenerateVirtualMachineCloudInit(_ context.Context, node string, vmid int) error {
	b.cloudInitRegenerated = vmid
	return nil
}

func (b *commandBackend) Networks(_ context.Context, node string, options pve.NetworkListOptions) ([]output.NetworkRow, error) {
	if err := b.networkErrs[node]; err != nil {
		return nil, err
	}
	rows := b.networkRows[node]
	out := make([]output.NetworkRow, 0, len(rows))
	for _, row := range rows {
		if options.Type != "" && row.Type != options.Type {
			continue
		}
		if options.Active && !row.Active {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func (b *commandBackend) Network(_ context.Context, node, iface string) (output.NetworkRow, error) {
	for _, row := range b.networkRows[node] {
		if row.Name == iface {
			return row, nil
		}
	}
	return output.NetworkRow{}, pve.ErrNotFound
}

func (b *commandBackend) FirewallStatus(_ context.Context, scope pve.FirewallScope) (output.FirewallStatusRow, error) {
	row, ok := b.firewallStatus[scope.Type]
	if !ok {
		return output.FirewallStatusRow{}, pve.ErrNotFound
	}
	return row, nil
}

func (b *commandBackend) FirewallRules(_ context.Context, scope pve.FirewallScope) ([]output.FirewallRuleRow, error) {
	return b.firewallRules[scope.Type], nil
}

type commandGuest struct {
	row              output.GuestRow
	task             pve.Task
	actionErr        error
	cloneID          int
	cloneOptions     pve.CloneOptions
	configValues     map[string]string
	deleted          bool
	rebooted         bool
	startedCalled    bool
	shutdownCalled   bool
	stopCalled       bool
	migrateOptions   pve.MigrateOptions
	resizeDisk       string
	resizeSize       string
	snapshots        []output.SnapshotRow
	createdSnapshot  string
	rollbackSnapshot string
	deletedSnapshot  string
}

func (g *commandGuest) Row() output.GuestRow {
	return g.row
}

func (g *commandGuest) Start(context.Context) (pve.Task, error) {
	g.startedCalled = true
	if g.actionErr != nil {
		return nil, g.actionErr
	}
	return g.task, nil
}

func (g *commandGuest) Shutdown(context.Context) (pve.Task, error) {
	g.shutdownCalled = true
	if g.actionErr != nil {
		return nil, g.actionErr
	}
	return g.task, nil
}

func (g *commandGuest) Stop(context.Context) (pve.Task, error) {
	g.stopCalled = true
	if g.actionErr != nil {
		return nil, g.actionErr
	}
	return g.task, nil
}

func (g *commandGuest) Reboot(context.Context) (pve.Task, error) {
	g.rebooted = true
	if g.actionErr != nil {
		return nil, g.actionErr
	}
	return g.task, nil
}

func (g *commandGuest) Clone(_ context.Context, options pve.CloneOptions) (pve.CloneResult, pve.Task, error) {
	g.cloneOptions = options
	name := options.Name
	if name == "" {
		name = options.Hostname
	}
	return pve.CloneResult{
		Kind:       g.row.Kind,
		SourceVMID: g.row.VMID,
		NewVMID:    uint64(g.cloneID),
		SourceNode: g.row.Node,
		TargetNode: options.Target,
		Name:       name,
		Task:       g.task.UPID(),
	}, g.task, nil
}

func (g *commandGuest) Config(_ context.Context, values map[string]string) (pve.Task, error) {
	g.configValues = values
	return g.task, nil
}

func (g *commandGuest) Delete(context.Context) (pve.Task, error) {
	g.deleted = true
	return g.task, nil
}

func (g *commandGuest) Migrate(_ context.Context, options pve.MigrateOptions) (pve.Task, error) {
	g.migrateOptions = options
	return g.task, nil
}

func (g *commandGuest) Resize(_ context.Context, disk, size string) (pve.Task, error) {
	g.resizeDisk = disk
	g.resizeSize = size
	return g.task, nil
}

func (g *commandGuest) Snapshots(context.Context) ([]output.SnapshotRow, error) {
	return g.snapshots, nil
}

func (g *commandGuest) CreateSnapshot(_ context.Context, name string) (pve.Task, error) {
	g.createdSnapshot = name
	return g.task, nil
}

func (g *commandGuest) RollbackSnapshot(_ context.Context, name string) (pve.Task, error) {
	g.rollbackSnapshot = name
	return g.task, nil
}

func (g *commandGuest) DeleteSnapshot(_ context.Context, name string) (pve.Task, error) {
	g.deletedSnapshot = name
	return g.task, nil
}

type commandTask struct {
	upid   string
	waited bool
}

func (t *commandTask) UPID() string {
	return t.upid
}

func (t *commandTask) WaitFor(context.Context, int) error {
	t.waited = true
	return nil
}

func (t *commandTask) ExitStatus() string {
	return ""
}

func (t *commandTask) Failed() bool {
	return false
}

func writeTestConfig(t *testing.T, defaultOutput string) string {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("PVECTL_TOKEN", "secret")
	cfg := config.Empty()
	if err := cfg.SetProfile("home", config.Profile{
		Endpoint:       "https://pve.example:8006/api2/json",
		TokenID:        "root@pam!test",
		TokenSecretEnv: "PVECTL_TOKEN",
		DefaultOutput:  defaultOutput,
	}); err != nil {
		t.Fatalf("set profile: %v", err)
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return cfgPath
}

func testDeps(stdout *bytes.Buffer, backend pve.Backend) Dependencies {
	return Dependencies{
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
		BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) {
			return backend, nil
		},
	}
}
