package pve

import (
	"context"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

func checkSourceBackend(running bool) Backend {
	source := &fakeBackend{
		nodes: []output.NodeRow{{Name: "n150", Status: "online"}},
	}
	status := "stopped"
	if running {
		status = "running"
	}
	guest := &fakeGuest{row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "n150", Status: status}}
	source.lxcs = map[string]map[int]*fakeGuest{"n150": {101: guest}}
	return source
}

type checkConfigBackend struct {
	Backend
	config map[string]any
}

func (b *checkConfigBackend) GuestConfig(context.Context, string, string, int) (map[string]any, error) {
	return b.config, nil
}

func checkTargetBackend(targetNode string) *fakeBackend {
	return &fakeBackend{
		nodes: []output.NodeRow{{Name: targetNode, Status: "online"}},
		lxcs: map[string]map[int]*fakeGuest{targetNode: {201: {
			row: output.GuestRow{Kind: "lxc", VMID: 201, Node: targetNode, Status: "stopped"},
		}}},
		lxcRows: map[string][]output.GuestRow{targetNode: {{Kind: "lxc", VMID: 201, Node: targetNode, Status: "stopped"}}},
		storageRows: map[string][]output.StorageRow{
			targetNode: {{Node: targetNode, Storage: "local-lvm", Type: "lvmthin", Active: true, Enabled: true, Content: "rootdir,images", Avail: 100 << 30}},
		},
		networkRows: map[string][]output.NetworkRow{
			targetNode: {{Node: targetNode, Name: "vmbr0", Type: "bridge", Active: true}},
		},
	}
}

// The healthy post-migration state: target stopped and ready to start,
// source retained but stopped, storage and bridges in place.
func TestCheckRemoteMigrationHealthy(t *testing.T) {
	sourceConfig := map[string]any{"net0": "name=eth0,bridge=vmbr2,hwaddr=BC:24:11:2F:AB:12"}
	target := checkTargetBackend("pve8845")
	targetConfig := map[string]any{
		"rootfs": "local-lvm:vm-201-disk-0,size=8G",
		"net0":   "name=eth0,bridge=vmbr0,hwaddr=BC:24:11:2F:AB:12",
		"onboot": "0",
	}

	plan := runCheckConfigurable(t, "stopped", sourceConfig, target, targetConfig)
	statuses := map[string]string{}
	for _, row := range plan.Checks {
		statuses[row.Check] = row.Status
	}
	for _, check := range []string{"source", "target", "target_config", "target_start", "mac_ip", "mount"} {
		if _, ok := statuses[check]; !ok {
			t.Fatalf("missing check %q: %+v", check, plan.Checks)
		}
	}
	if statuses["target"] != "ok" || statuses["target_config"] != "ok" || statuses["target_start"] != "ok" {
		t.Fatalf("healthy checks failed: %+v", statuses)
	}
	if statuses["source"] != "warning" {
		t.Fatalf("source = %q, want warning for retained stopped source", statuses["source"])
	}
	if statuses["mac_ip"] != "unknown" {
		t.Fatalf("mac_ip = %q, want unknown", statuses["mac_ip"])
	}
}

// The classic split-brain risk: source still running with the same MAC as
// the target container.
func TestCheckRemoteMigrationDuplicateMACWhileSourceRuns(t *testing.T) {
	const mac = "BC:24:11:2F:AB:12"
	sourceConfig := map[string]any{"net0": "name=eth0,bridge=vmbr2,hwaddr=" + mac}
	targetConfig := map[string]any{"net0": "name=eth0,bridge=vmbr0,hwaddr=" + mac}

	plan := runCheckConfigurable(t, "running", sourceConfig, checkTargetBackend("pve8845"), targetConfig)
	if findCheck(plan, "mac_ip").Status != "fail" {
		t.Fatalf("mac_ip = %+v, want fail for duplicate MAC with running source", findCheck(plan, "mac_ip"))
	}
	if plan.Status != "failed" {
		t.Fatalf("overall = %q", plan.Status)
	}
}

// Source removed entirely counts as clean.
func TestCheckRemoteMigrationSourceRemoved(t *testing.T) {
	plan := runCheckConfigurable(t, "absent", nil, checkTargetBackend("pve8845"),
		map[string]any{"rootfs": "local-lvm:vm-201-disk-0,size=8G", "net0": "name=eth0,bridge=vmbr0"})
	if findCheck(plan, "source").Status != "ok" {
		t.Fatalf("source = %+v", findCheck(plan, "source"))
	}
}

// Missing target guest fails the check.
func TestCheckRemoteMigrationTargetMissing(t *testing.T) {
	target := checkTargetBackend("pve8845")
	target.lxcs = map[string]map[int]*fakeGuest{"pve8845": {}}
	target.lxcRows = map[string][]output.GuestRow{"pve8845": {}}

	plan := runCheckConfigurable(t, "stopped", nil, target, nil)
	if findCheck(plan, "target").Status != "fail" {
		t.Fatalf("target = %+v", findCheck(plan, "target"))
	}
}

// Target storage missing on the node surfaces as a config failure.
func TestCheckRemoteMigrationTargetStorageMissing(t *testing.T) {
	target := checkTargetBackend("pve8845")
	target.storageRows = map[string][]output.StorageRow{"pve8845": {}}

	plan := runCheckConfigurable(t, "stopped", nil, target,
		map[string]any{"rootfs": "gone-storage:vm-201-disk-0,size=8G", "net0": "name=eth0,bridge=vmbr0"})
	if findCheck(plan, "target_config").Status != "fail" {
		t.Fatalf("target_config = %+v", findCheck(plan, "target_config"))
	}
}

// Locks block a start.
func TestCheckRemoteMigrationTargetLocked(t *testing.T) {
	targetConfig := map[string]any{"lock": "backup", "rootfs": "local-lvm:x,size=8G"}

	plan := runCheckConfigurable(t, "stopped", nil, checkTargetBackend("pve8845"), targetConfig)
	if findCheck(plan, "target_start").Status != "fail" {
		t.Fatalf("target_start = %+v", findCheck(plan, "target_start"))
	}
}

// Bind mounts and device passthrough keep requiring manual attention.
func TestCheckRemoteMigrationMountsStillManual(t *testing.T) {
	plan := runCheckConfigurable(t, "stopped", nil, checkTargetBackend("pve8845"),
		map[string]any{"rootfs": "local-lvm:x,size=8G", "mp0": "/mnt/data:mp=/host/data"})
	if findCheck(plan, "mount").Status != "warning" {
		t.Fatalf("mount = %+v", findCheck(plan, "mount"))
	}
}

// runCheckConfigurable wires the config backends for both sides.
// sourceState: "running", "stopped" or "absent".
func runCheckConfigurable(t *testing.T, sourceState string, sourceConfig map[string]any, target Backend, targetConfig map[string]any) output.MigrationPlan {
	t.Helper()
	var source Backend
	switch sourceState {
	case "absent":
		source = &fakeBackend{nodes: []output.NodeRow{{Name: "n150", Status: "online"}}}
	default:
		source = checkSourceBackend(sourceState == "running")
	}
	sourceSvc := NewLXCService(&checkConfigBackend{Backend: source, config: sourceConfig}, TaskRunner{}, nil, false)
	targetWithConfig := &checkConfigBackend{Backend: target, config: targetConfig}
	plan, err := sourceSvc.CheckRemoteMigration(context.Background(), 201, RemoteMigrationCheckOptions{
		SourceVMID: 101,
		Target:     targetWithConfig,
		TargetNode: "pve8845",
	})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	return plan
}
