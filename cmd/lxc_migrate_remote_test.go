package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

// The remote command runs the preflight plan first, transfers credentials
// only with --yes, and never echoes the target token.
func TestLXCMigrateRemoteCommandEndToEnd(t *testing.T) {
	backend := migrationPlanBackends()
	guest := &commandGuest{row: backend.source.lxcGuests["n150"][101].row}
	backend.source.lxcGuests["n150"][101] = guest
	var stdout, stderr bytes.Buffer
	deps := migrationDeps(backend, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "migrate", "remote", "101",
		"--to", "pve8845", "--target-node", "pve8845", "--target-vmid", "201",
		"--storage-map", "local-lvm=local-lvm", "--bridge-map", "vmbr2=vmbr0",
		"--yes", "-o", "json",
	}, "test", deps)
	if err != nil {
		t.Fatalf("remote migrate: %v (stderr %s)", err, stderr.String())
	}
	if guest.remoteMigrateOptions == nil {
		t.Fatal("remote migrate not submitted")
	}
	options := guest.remoteMigrateOptions
	if options.TargetVMID != 201 || options.Online {
		t.Fatalf("options = %+v", options)
	}
	if !strings.Contains(options.TargetEndpointBundle, "host=pve8845.lan:8006") {
		t.Fatalf("bundle = %q", options.TargetEndpointBundle)
	}
	if !strings.Contains(options.TargetEndpointBundle, "apitoken=PVEAPIToken=root@pam!test=") {
		t.Fatalf("bundle missing token reference: %q", options.TargetEndpointBundle)
	}
	for _, stream := range []string{stdout.String(), stderr.String()} {
		if strings.Contains(stream, "apitoken=PVEAPIToken") {
			t.Fatalf("target credentials leaked into output: %s", stream)
		}
	}
	if !strings.Contains(stderr.String(), "receives target API credentials") {
		t.Fatalf("credential notice missing: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "experimental") {
		t.Fatalf("experimental notice missing: %s", stderr.String())
	}
}

func TestLXCMigrateRemoteRequiresYes(t *testing.T) {
	backend := migrationPlanBackends()
	guest := &commandGuest{row: backend.source.lxcGuests["n150"][101].row}
	backend.source.lxcGuests["n150"][101] = guest
	var stdout, stderr bytes.Buffer
	deps := migrationDeps(backend, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "migrate", "remote", "101",
		"--to", "pve8845", "--target-node", "pve8845",
	}, "test", deps)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v", err)
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if guest.remoteMigrateOptions != nil {
		t.Fatal("no submission without --yes")
	}
}

// A failing preflight aborts before any mutation.
func TestLXCMigrateRemoteAbortsOnFailedPreflight(t *testing.T) {
	backend := migrationPlanBackends()
	backend.target.networkRows = map[string][]output.NetworkRow{} // bridge missing
	guest := &commandGuest{row: backend.source.lxcGuests["n150"][101].row}
	backend.source.lxcGuests["n150"][101] = guest
	var stdout, stderr bytes.Buffer
	deps := migrationDeps(backend, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "migrate", "remote", "101",
		"--to", "pve8845", "--target-node", "pve8845", "--yes",
	}, "test", deps)
	if err == nil || !strings.Contains(err.Error(), "preflight") {
		t.Fatalf("err = %v", err)
	}
	if guest.remoteMigrateOptions != nil {
		t.Fatal("mutation must not run after failed preflight")
	}
}
