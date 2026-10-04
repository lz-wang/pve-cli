package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lz-wang/pvectl/internal/output"
)

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

func TestGuestBulkWithoutDryRunRefuses(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms: map[string][]output.GuestRow{
			"pve1": {{Kind: "vm", VMID: 100, Name: "debian", Node: "pve1", Status: "running", Tags: "infra"}},
		},
	}

	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"guest", "shutdown",
		"--tag", "infra",
	}, "test", testDeps(&bytes.Buffer{}, backend))
	if err == nil {
		t.Fatal("expected refusal without --dry-run")
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
