package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/lz-wang/pvectl/internal/output"
)

func bulkTestBackend() *commandBackend {
	return &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms: map[string][]output.GuestRow{
			"pve1": {
				{Kind: "vm", VMID: 100, Name: "debian", Node: "pve1", Status: "running", Tags: "infra"},
				{Kind: "vm", VMID: 101, Name: "play", Node: "pve1", Status: "running", Tags: "sandbox"},
			},
		},
		lxcs: map[string][]output.GuestRow{
			"pve1": {{Kind: "lxc", VMID: 200, Name: "agh", Node: "pve1", Status: "running", Tags: "infra"}},
		},
		vmGuests: map[string]map[int]*commandGuest{
			"pve1": {
				100: {row: output.GuestRow{Kind: "vm", VMID: 100, Node: "pve1"}, task: &commandTask{upid: "UPID:pve1:b100"}},
				101: {row: output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"}, task: &commandTask{upid: "UPID:pve1:b101"}},
			},
		},
		lxcGuests: map[string]map[int]*commandGuest{
			"pve1": {200: {row: output.GuestRow{Kind: "lxc", VMID: 200, Node: "pve1"}, task: &commandTask{upid: "UPID:pve1:b200"}}},
		},
	}
}

func TestGuestBulkShutdownDryRunListsPlan(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms: map[string][]output.GuestRow{
			"pve1": {
				{Kind: "vm", VMID: 100, Name: "debian", Node: "pve1", Status: "running", Tags: "infra"},
				{Kind: "vm", VMID: 101, Name: "play", Node: "pve1", Status: "running", Tags: "sandbox"},
			},
		},
		lxcs: map[string][]output.GuestRow{
			"pve1": {{Kind: "lxc", VMID: 200, Name: "agh", Node: "pve1", Status: "running", Tags: "infra"}},
		},
	}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"guest", "shutdown",
		"--tag", "infra",
		"--dry-run",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, `"vmid": 100`) || !strings.Contains(out, `"vmid": 200`) || strings.Contains(out, `"vmid": 101`) {
		t.Fatalf("dry-run plan = %s", out)
	}
}

func TestGuestBulkShutdownExecutesWithForce(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	backend := bulkTestBackend()

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"guest", "shutdown",
		"--tag", "infra",
		"--force",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !backend.vmGuests["pve1"][100].shutdownCalled || !backend.lxcGuests["pve1"][200].shutdownCalled {
		t.Fatal("expected matched guests to be shut down")
	}
	if backend.vmGuests["pve1"][101].shutdownCalled {
		t.Fatal("guest outside selection must not be touched")
	}
}

func TestGuestBulkExecutesAfterConfirmation(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	backend := bulkTestBackend()
	deps := testDeps(&bytes.Buffer{}, backend)
	deps.Stdin = strings.NewReader("no\n")

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"guest", "shutdown",
		"--tag", "infra",
	}, "test", deps)
	if err == nil {
		t.Fatal("expected abort on wrong confirmation")
	}
	if backend.lxcGuests["pve1"][200].shutdownCalled {
		t.Fatal("no guest should be touched after abort")
	}

	backend = bulkTestBackend()
	deps = testDeps(&bytes.Buffer{}, backend)
	deps.Stdin = strings.NewReader("yes\n")

	err = RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"guest", "shutdown",
		"--tag", "infra",
	}, "test", deps)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !backend.lxcGuests["pve1"][200].shutdownCalled || !backend.vmGuests["pve1"][100].shutdownCalled {
		t.Fatal("expected guests to be shut down after confirmation")
	}
}

func TestGuestBulkSingleGuestSkipsConfirmation(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	backend := bulkTestBackend()
	deps := testDeps(&bytes.Buffer{}, backend)
	deps.Stdin = strings.NewReader("")

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"guest", "stop",
		"--tag", "sandbox",
	}, "test", deps)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !backend.vmGuests["pve1"][101].stopCalled {
		t.Fatal("expected single matching guest to be stopped without confirmation")
	}
}

func TestGuestBulkPartialFailureWritesAllResultsAndFails(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	backend := bulkTestBackend()
	backend.vmGuests["pve1"][100].actionErr = errors.New("boom")
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"guest", "shutdown",
		"--tag", "infra",
		"--force",
	}, "test", testDeps(&stdout, backend))
	if err == nil {
		t.Fatal("expected aggregated failure")
	}

	out := stdout.String()
	if !strings.Contains(out, `"vmid": 100`) || !strings.Contains(out, `"status": "error"`) {
		t.Fatalf("missing failed result: %s", out)
	}
	if !strings.Contains(out, `"vmid": 200`) || !strings.Contains(out, `"status": "ok"`) {
		t.Fatalf("missing successful result: %s", out)
	}
}

func TestGuestBulkWithoutScopeFails(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"guest", "shutdown",
		"--dry-run",
	}, "test", testDeps(&bytes.Buffer{}, &commandBackend{}))
	if err == nil {
		t.Fatal("expected missing scope error")
	}
}
