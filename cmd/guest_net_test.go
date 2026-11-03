package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
)

func netSetTestBackend() (*commandBackend, *commandGuest) {
	lxcRow := output.GuestRow{Kind: "lxc", VMID: 200, Name: "app", Node: "pve1", Status: "stopped"}
	guest := &commandGuest{row: lxcRow}
	backend := &commandBackend{
		nodes:     []output.NodeRow{{Name: "pve1"}},
		lxcs:      map[string][]output.GuestRow{"pve1": {lxcRow}},
		lxcGuests: map[string]map[int]*commandGuest{"pve1": {200: guest}},
		networkRows: map[string][]output.NetworkRow{
			"pve1": {{Node: "pve1", Name: "vmbr0", Type: "bridge"}},
		},
		guestConfigs: map[int]map[string]any{
			200: {"net0": "name=eth0,bridge=vmbr2,ip=192.168.2.10/24", "digest": "d1"},
		},
	}
	return backend, guest
}

func TestLXCNetSetCommandSubmitsMergedDeviceValue(t *testing.T) {
	backend, guest := netSetTestBackend()
	var stdout, stderr bytes.Buffer
	err := RunWithDependencies([]string{
		"pve", "--config", writeTestConfig(t, "json"),
		"lxc", "net", "set", "200",
		"--node", "pve1",
		"--device", "net0",
		"--bridge", "vmbr0",
		"-o", "json",
	}, "test", commandDeps(&stdout, &stderr, backend))
	if err != nil {
		t.Fatalf("net set: %v (stderr %s)", err, stderr.String())
	}
	submitted := guest.configValues["net0"]
	if submitted != "name=eth0,bridge=vmbr0,ip=192.168.2.10/24" {
		t.Fatalf("submitted net0 = %q", submitted)
	}
	if guest.configValues["digest"] != "d1" {
		t.Fatalf("digest = %v", guest.configValues["digest"])
	}
	if !strings.Contains(stdout.String(), `"verified": true`) {
		t.Fatalf("stdout missing verified: %s", stdout.String())
	}
}

func TestLXCNetSetCommandRejectsUnknownBridge(t *testing.T) {
	backend, _ := netSetTestBackend()
	var stdout, stderr bytes.Buffer
	err := RunWithDependencies([]string{
		"pve", "--config", writeTestConfig(t, "json"),
		"lxc", "net", "set", "200",
		"--node", "pve1",
		"--device", "net0",
		"--bridge", "vmbr9",
	}, "test", commandDeps(&stdout, &stderr, backend))
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err = %v", err)
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if backend.backupCalls != 0 {
		t.Fatal("no mutation expected")
	}
}

func TestLXCNetSetCommandRequiresDeviceAndAttribute(t *testing.T) {
	for name, args := range map[string][]string{
		"missing device":    {"--bridge", "vmbr0"},
		"missing attribute": {"--device", "net0"},
	} {
		t.Run(name, func(t *testing.T) {
			backend, _ := netSetTestBackend()
			var stdout, stderr bytes.Buffer
			full := append([]string{
				"pve", "--config", writeTestConfig(t, "json"),
				"lxc", "net", "set", "200", "--node", "pve1",
			}, args...)
			err := RunWithDependencies(full, "test", commandDeps(&stdout, &stderr, backend))
			if err == nil || !strings.Contains(err.Error(), pve.ErrInvalid.Error()) {
				t.Fatalf("err = %v", err)
			}
			if code := ExitCode(err); code != 2 {
				t.Fatalf("exit code = %d, want 2", code)
			}
		})
	}
}
