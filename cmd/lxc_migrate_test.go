package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
)

func writeTwoProfileConfig(t *testing.T) string {
	t.Helper()
	cfgPath := t.TempDir() + "/config.yaml"
	t.Setenv("PVE_TOKEN", "secret")
	cfg := config.Empty()
	for _, name := range []string{"n150", "pve8845"} {
		if err := cfg.SetProfile(name, config.Profile{
			Endpoint:       "https://" + name + ".lan:8006",
			TokenID:        "root@pam!test",
			TokenSecretEnv: "PVE_TOKEN",
		}); err != nil {
			t.Fatalf("set profile %s: %v", name, err)
		}
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

type migrationTestBackends struct {
	source *commandBackend
	target *commandBackend
}

func migrationPlanBackends() *migrationTestBackends {
	lxcRow := output.GuestRow{Kind: "lxc", VMID: 101, Name: "app", Node: "n150", Status: "running"}
	source := &commandBackend{
		nodes: []output.NodeRow{{Name: "n150", Status: "online"}},
		lxcs:  map[string][]output.GuestRow{"n150": {lxcRow}},
		lxcGuests: map[string]map[int]*commandGuest{
			"n150": {101: {row: lxcRow}},
		},
		guestConfigs: map[int]map[string]any{
			101: {"rootfs": "local-lvm:vm-101-disk-0,size=8G", "net0": "name=eth0,bridge=vmbr2", "digest": "d"},
		},
	}
	target := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve8845", Status: "online"}},
		vms:   map[string][]output.GuestRow{},
		lxcs:  map[string][]output.GuestRow{},
		storages: map[string][]output.StorageRow{
			"pve8845": {{Node: "pve8845", Storage: "local-lvm", Type: "lvmthin", Active: true, Enabled: true, Content: "rootdir,images", Avail: 100 << 30, Total: 200 << 30}},
		},
		networkRows: map[string][]output.NetworkRow{
			"pve8845": {{Node: "pve8845", Name: "vmbr0", Type: "bridge", Active: true}},
		},
	}
	return &migrationTestBackends{source: source, target: target}
}

func migrationDeps(backend *migrationTestBackends, stdout, stderr *bytes.Buffer) Dependencies {
	deps := Dependencies{Stdout: stdout, Stderr: stderr}
	deps.BackendFactory = func(profile config.Profile, _ pve.ClientOptions) (pve.Backend, error) {
		if strings.Contains(profile.Endpoint, "pve8845") {
			return backend.target, nil
		}
		return backend.source, nil
	}
	return deps
}

func TestLXCMigratePlanCommandRunsChecks(t *testing.T) {
	backend := migrationPlanBackends()
	var stdout, stderr bytes.Buffer
	deps := migrationDeps(backend, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "migrate", "plan", "101",
		"--to", "pve8845",
		"--target-node", "pve8845",
		"--target-vmid", "201",
		"--storage-map", "local-lvm=local-lvm",
		"--bridge-map", "vmbr2=vmbr0",
		"-o", "json",
	}, "test", deps)
	if err != nil {
		t.Fatalf("plan: %v (stderr %s)", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{`"resource": "lxc"`, `"target_node": "pve8845"`, `"target_vmid": 201`, `"check": "profile"`, `"status": "warning"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

// The legacy intra-cluster migrate still works with a VMID argument.
func TestLXCMigrateLegacyCommandStillWorks(t *testing.T) {
	backend := migrationPlanBackends()
	guest := &commandGuest{row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "n150", Status: "stopped"}, task: nil}
	backend.source.lxcGuests["n150"][101] = guest
	var stdout, stderr bytes.Buffer
	deps := migrationDeps(backend, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "migrate", "101", "--target", "other", "-o", "json",
	}, "test", deps)
	if err != nil {
		t.Fatalf("legacy migrate: %v", err)
	}
	if guest.migrateOptions.Target != "other" {
		t.Fatalf("migrate options = %+v", guest.migrateOptions)
	}
}

func TestLXCMigratePlanCommandFailsExitOne(t *testing.T) {
	backend := migrationPlanBackends()
	// Target VMID collision: 101 exists on target.
	backend.target.lxcs = map[string][]output.GuestRow{"pve8845": {{Kind: "lxc", VMID: 101, Node: "pve8845"}}}
	var stdout, stderr bytes.Buffer
	deps := migrationDeps(backend, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "migrate", "plan", "101",
		"--to", "pve8845", "--target-node", "pve8845",
		"-o", "json",
	}, "test", deps)
	if err == nil {
		t.Fatal("expected plan failure")
	}
	if code := ExitCode(err); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), `"status": "failed"`) {
		t.Fatalf("plan output missing failed status:\n%s", stdout.String())
	}
}

func TestLXCMigratePlanRejectsSameProfile(t *testing.T) {
	backend := migrationPlanBackends()
	var stdout, stderr bytes.Buffer
	deps := migrationDeps(backend, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "migrate", "plan", "101",
		"--to", "n150", "--target-node", "pve8845",
	}, "test", deps)
	if err == nil || !strings.Contains(err.Error(), "different profile") {
		t.Fatalf("err = %v", err)
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestLXCMigratePlanRequiresNumericVMID(t *testing.T) {
	backend := migrationPlanBackends()
	var stdout, stderr bytes.Buffer
	deps := migrationDeps(backend, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "migrate", "plan", "app",
		"--to", "pve8845", "--target-node", "pve8845",
	}, "test", deps)
	if err == nil || !strings.Contains(err.Error(), "numeric") {
		t.Fatalf("err = %v", err)
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}
