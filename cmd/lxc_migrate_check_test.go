package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

// Post-migration validation against both clusters with a healthy target
// and a retained stopped source.
func TestLXCMigrateCheckCommandHealthy(t *testing.T) {
	backend := migrationPlanBackends()
	// Target guest 201 after migration.
	targetRow := output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve8845", Status: "stopped"}
	backend.target.lxcs = map[string][]output.GuestRow{"pve8845": {targetRow}}
	backend.target.lxcGuests = map[string]map[int]*commandGuest{"pve8845": {201: {row: targetRow}}}
	backend.target.guestConfigs = map[int]map[string]any{
		201: {"rootfs": "local-lvm:vm-201-disk-0,size=8G", "net0": "name=eth0,bridge=vmbr0"},
	}
	// Source retained, stopped (different MAC is not required; same MAC
	// with a stopped source is only unknown, not a failure).
	backend.source.lxcGuests["n150"][101] = &commandGuest{row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "n150", Status: "stopped"}}

	var stdout, stderr bytes.Buffer
	deps := migrationDeps(backend, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "migrate", "check", "201",
		"--to", "pve8845", "--target-node", "pve8845", "--source-vmid", "101",
		"-o", "json",
	}, "test", deps)
	if err != nil {
		t.Fatalf("check: %v (stderr %s)", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{`"check": "source"`, `"check": "target"`, `"check": "target_start"`, `"check": "mac_ip"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"status": "failed"`) {
		t.Fatalf("healthy validation reported failed:\n%s", out)
	}
}

// A missing target guest fails validation with exit code 1.
func TestLXCMigrateCheckCommandTargetMissing(t *testing.T) {
	backend := migrationPlanBackends()
	backend.target.lxcs = map[string][]output.GuestRow{"pve8845": {}}
	backend.target.lxcGuests = map[string]map[int]*commandGuest{"pve8845": {}}

	var stdout, stderr bytes.Buffer
	deps := migrationDeps(backend, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "migrate", "check", "201",
		"--to", "pve8845", "--target-node", "pve8845",
	}, "test", deps)
	if err == nil || !strings.Contains(err.Error(), "validation failed") {
		t.Fatalf("err = %v", err)
	}
	if code := ExitCode(err); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}
