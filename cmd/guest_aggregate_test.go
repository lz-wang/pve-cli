package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lz-wang/pvectl/internal/config"
	"github.com/lz-wang/pvectl/internal/output"
	"github.com/lz-wang/pvectl/internal/pve"
)

func TestGuestListCommandTagFiltering(t *testing.T) {
	cfgPath := writeTestConfig(t, "json")
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms: map[string][]output.GuestRow{
			"pve1": {
				{Kind: "vm", VMID: 100, Name: "debian", Node: "pve1", Status: "running", Tags: "infra;docker"},
				{Kind: "vm", VMID: 101, Name: "play", Node: "pve1", Status: "running", Tags: "sandbox"},
			},
		},
		lxcs: map[string][]output.GuestRow{
			"pve1": {{Kind: "lxc", VMID: 200, Name: "agh", Node: "pve1", Status: "running", Tags: "infra;production"}},
		},
	}

	var stdout bytes.Buffer
	err := RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"guest", "ls",
		"--tag", "infra",
		"--tag", "docker",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, `"vmid": 100`) || strings.Contains(out, `"vmid": 101`) || strings.Contains(out, `"vmid": 200`) {
		t.Fatalf("all-match filter failed: %s", out)
	}

	stdout.Reset()
	err = RunWithDependencies([]string{
		"pvectl", "--config", cfgPath,
		"guest", "ls",
		"--tag", "infra",
		"--tag", "production",
		"--tag-match", "any",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	out = stdout.String()
	if !strings.Contains(out, `"vmid": 100`) || !strings.Contains(out, `"vmid": 200`) || strings.Contains(out, `"vmid": 101`) {
		t.Fatalf("any-match filter failed: %s", out)
	}
}

func TestGuestListCommandWritesTableWithKind(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string][]output.GuestRow{"pve1": {{Kind: "vm", VMID: 100, Name: "debian", Node: "pve1", Status: "running"}}},
		lxcs:  map[string][]output.GuestRow{"pve1": {{Kind: "lxc", VMID: 200, Name: "app", Node: "pve1", Status: "stopped"}}},
	}

	err := RunWithDependencies([]string{"pvectl", "--config", cfgPath, "guest", "ls"}, "test", commandDeps(&stdout, &stderr, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "KIND") || !strings.Contains(got, "vm") || !strings.Contains(got, "lxc") {
		t.Fatalf("stdout = %s", got)
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestGuestListCommandFiltersTypeVM(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	var stdout bytes.Buffer
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string][]output.GuestRow{"pve1": {{Kind: "vm", VMID: 100, Name: "debian", Node: "pve1", Status: "running"}}},
		lxcs:  map[string][]output.GuestRow{"pve1": {{Kind: "lxc", VMID: 200, Name: "app-lxc", Node: "pve1", Status: "running"}}},
	}

	err := RunWithDependencies([]string{"pvectl", "--config", cfgPath, "guest", "ls", "--type", "vm"}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "debian") || strings.Contains(got, "app-lxc") {
		t.Fatalf("stdout = %s", got)
	}
}

func TestGuestListCommandWritesJSON(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	var stdout bytes.Buffer
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string][]output.GuestRow{"pve1": {{Kind: "vm", VMID: 100, Name: "debian", Node: "pve1"}}},
		lxcs:  map[string][]output.GuestRow{"pve1": {{Kind: "lxc", VMID: 200, Name: "app", Node: "pve1"}}},
	}

	err := RunWithDependencies([]string{"pvectl", "--config", cfgPath, "guest", "ls", "-o", "json"}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, `"kind": "vm"`) || !strings.Contains(got, `"kind": "lxc"`) {
		t.Fatalf("stdout = %s", got)
	}
}

func TestGuestGetCommandAuto(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	var stdout bytes.Buffer
	backend := &commandBackend{
		nodes:    []output.NodeRow{{Name: "pve1"}},
		vmGuests: map[string]map[int]*commandGuest{"pve1": {100: {row: output.GuestRow{Kind: "vm", VMID: 100, Name: "debian", Node: "pve1"}}}},
	}

	err := RunWithDependencies([]string{"pvectl", "--config", cfgPath, "guest", "get", "100"}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "Kind:") || !strings.Contains(got, "vm") || !strings.Contains(got, "debian") {
		t.Fatalf("stdout = %s", got)
	}
}

func TestGuestGetCommandTypeAndTrailingFlags(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	var stdout bytes.Buffer
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vmGuests: map[string]map[int]*commandGuest{
			"pve1": {300: {row: output.GuestRow{Kind: "vm", VMID: 300, Name: "shared-vm", Node: "pve1"}}},
		},
		lxcGuests: map[string]map[int]*commandGuest{
			"pve1": {300: {row: output.GuestRow{Kind: "lxc", VMID: 300, Name: "shared-lxc", Node: "pve1"}}},
		},
	}

	err := RunWithDependencies([]string{"pvectl", "--config", cfgPath, "guest", "get", "300", "--type", "vm", "-o", "json"}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, `"kind": "vm"`) || strings.Contains(got, "shared-lxc") {
		t.Fatalf("stdout = %s", got)
	}
}

func TestGuestGetCommandAmbiguous(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	backend := &commandBackend{
		nodes:     []output.NodeRow{{Name: "pve1"}},
		vmGuests:  map[string]map[int]*commandGuest{"pve1": {300: {row: output.GuestRow{Kind: "vm", VMID: 300, Node: "pve1"}}}},
		lxcGuests: map[string]map[int]*commandGuest{"pve1": {300: {row: output.GuestRow{Kind: "lxc", VMID: 300, Node: "pve1"}}}},
	}

	err := RunWithDependencies([]string{"pvectl", "--config", cfgPath, "guest", "get", "300"}, "test", testDeps(&bytes.Buffer{}, backend))
	if err == nil || !strings.Contains(err.Error(), "guest 300 is ambiguous") {
		t.Fatalf("error = %v", err)
	}
}

func TestGuestCommandInvalidType(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")

	err := RunWithDependencies([]string{"pvectl", "--config", cfgPath, "guest", "ls", "--type", "foo"}, "test", testDeps(&bytes.Buffer{}, &commandBackend{}))
	if err == nil || err.Error() != `invalid guest type "foo", expected all, vm, or lxc` {
		t.Fatalf("error = %v", err)
	}
}

func commandDeps(stdout, stderr *bytes.Buffer, backend pve.Backend) Dependencies {
	return Dependencies{
		Stdout: stdout,
		Stderr: stderr,
		BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) {
			return backend, nil
		},
	}
}
