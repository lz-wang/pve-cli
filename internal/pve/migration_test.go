package pve

import (
	"context"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

func migrationSourceBackend() *fakeBackend {
	return &fakeBackend{
		nodes: []output.NodeRow{{Name: "n150", Status: "online"}},
		lxcs: map[string]map[int]*fakeGuest{"n150": {101: {
			row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "n150", Name: "app", Status: "running"},
		}}},
	}
}

type migrationConfigBackend struct {
	*fakeBackend
	config map[string]any
	calls  int
}

func (b *migrationConfigBackend) GuestConfig(context.Context, string, string, int) (map[string]any, error) {
	b.calls++
	return b.config, nil
}

func migrationTargetBackend() *fakeBackend {
	return &fakeBackend{
		nodes:   []output.NodeRow{{Name: "pve8845", Status: "online"}},
		vms:     map[string]map[int]*fakeGuest{},
		lxcs:    map[string]map[int]*fakeGuest{"pve8845": {300: {row: output.GuestRow{Kind: "lxc", VMID: 300, Node: "pve8845"}}}},
		lxcRows: map[string][]output.GuestRow{"pve8845": {{Kind: "lxc", VMID: 300, Node: "pve8845", Status: "stopped"}}},
		storageRows: map[string][]output.StorageRow{
			"pve8845": {
				{Node: "pve8845", Storage: "local-lvm", Type: "lvmthin", Active: true, Enabled: true, Content: "rootdir,images", Avail: 100 << 30, Total: 200 << 30},
			},
		},
		networkRows: map[string][]output.NetworkRow{
			"pve8845": {{Node: "pve8845", Name: "vmbr0", Type: "bridge", Active: true}},
		},
	}
}

func sourceService(source Backend) *GuestService {
	return NewLXCService(source, TaskRunner{}, nil, false)
}

func planOptions(target Backend) CrossProfileMigrationOptions {
	return CrossProfileMigrationOptions{
		TargetNode: "pve8845",
		TargetVMID: 201,
		Target:     target,
	}
}

// A complete, healthy migration plan yields per-dimension checks and an
// overall ok status. MAC/IP visibility stays explicitly unknown.
func TestPlanRemoteMigrationHappyPath(t *testing.T) {
	source := &migrationConfigBackend{fakeBackend: migrationSourceBackend(), config: map[string]any{
		"rootfs": "local-lvm:vm-101-disk-0,size=8G",
		"net0":   "name=eth0,bridge=vmbr2,hwaddr=BC:24:11:2F:AB:12",
		"onboot": "0",
	}}
	target := migrationTargetBackend()

	plan, err := sourceService(source).PlanRemoteMigration(context.Background(), 101, CrossProfileMigrationOptions{
		TargetNode: "pve8845",
		TargetVMID: 201,
		Target:     target,
		StorageMap: map[string]string{"local-lvm": "local-lvm"},
		BridgeMap:  map[string]string{"vmbr2": "vmbr0"},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Resource != "lxc" || plan.VMID != 101 || plan.Node != "n150" || plan.TargetNode != "pve8845" || plan.TargetVMID != 201 {
		t.Fatalf("plan = %+v", plan)
	}
	statuses := map[string]string{}
	for _, row := range plan.Checks {
		statuses[row.Check] = row.Status
	}
	for _, check := range []string{"profile", "vmid", "storage", "network", "mac_ip", "onboot", "mount", "state"} {
		if _, ok := statuses[check]; !ok {
			t.Fatalf("missing check %q in %+v", check, plan.Checks)
		}
	}
	if statuses["profile"] != "ok" || statuses["vmid"] != "ok" || statuses["storage"] != "ok" || statuses["network"] != "ok" {
		t.Fatalf("expected healthy checks, got %+v", statuses)
	}
	if statuses["mac_ip"] != "unknown" {
		t.Fatalf("mac_ip = %q, want unknown (API cannot observe guest-internal IPs)", statuses["mac_ip"])
	}
	if statuses["state"] != "warning" {
		t.Fatalf("state = %q, want warning for running guest", statuses["state"])
	}
	if plan.Status != "warning" {
		t.Fatalf("overall = %q, want warning (running source)", plan.Status)
	}
}

func TestPlanRemoteMigrationTargetVMIDTaken(t *testing.T) {
	source := &migrationConfigBackend{fakeBackend: migrationSourceBackend(), config: map[string]any{"rootfs": "local-lvm:vm-101-disk-0,size=8G"}}
	target := migrationTargetBackend()

	plan, err := sourceService(source).PlanRemoteMigration(context.Background(), 101, CrossProfileMigrationOptions{
		TargetNode: "pve8845",
		TargetVMID: 300, // exists on target
		Target:     target,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if findCheck(plan, "vmid").Status != "fail" || plan.Status != "failed" {
		t.Fatalf("vmid check = %+v overall = %q", findCheck(plan, "vmid"), plan.Status)
	}
}

func TestPlanRemoteMigrationTargetNodeMissing(t *testing.T) {
	source := &migrationConfigBackend{fakeBackend: migrationSourceBackend(), config: map[string]any{}}
	target := migrationTargetBackend()

	plan, err := sourceService(source).PlanRemoteMigration(context.Background(), 101, CrossProfileMigrationOptions{
		TargetNode: "nope",
		Target:     target,
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if findCheck(plan, "profile").Status != "fail" {
		t.Fatalf("profile check = %+v", findCheck(plan, "profile"))
	}
	if plan.Status != "failed" {
		t.Fatalf("overall = %q", plan.Status)
	}
}

// Unmapped storage that does not exist on the target and unmapped missing
// bridges fail the plan.
func TestPlanRemoteMigrationStorageAndNetworkFailures(t *testing.T) {
	source := &migrationConfigBackend{fakeBackend: migrationSourceBackend(), config: map[string]any{
		"rootfs": "zfs-fast:vm-101-disk-0,size=8G",
		"mp0":    "/mnt/backups:mp=/host/backups",
		"net0":   "name=eth0,bridge=vmbr2",
	}}
	target := migrationTargetBackend()

	plan, err := sourceService(source).PlanRemoteMigration(context.Background(), 101, planOptions(target))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if findCheck(plan, "storage").Status != "fail" {
		t.Fatalf("storage = %+v", findCheck(plan, "storage"))
	}
	if findCheck(plan, "network").Status != "fail" {
		t.Fatalf("network = %+v", findCheck(plan, "network"))
	}
	if findCheck(plan, "mount").Status != "warning" {
		t.Fatalf("mount = %+v (bind mount must warn)", findCheck(plan, "mount"))
	}
}

// Capacity comparison: target storage smaller than the source usage warns.
func TestPlanRemoteMigrationCapacityCheck(t *testing.T) {
	source := &migrationConfigBackend{fakeBackend: migrationSourceBackend(), config: map[string]any{
		"rootfs": "local-lvm:vm-101-disk-0,size=800G",
	}}
	target := migrationTargetBackend() // local-lvm has 100GiB avail
	target.storageRows["pve8845"][0].Storage = "local-lvm"

	plan, err := sourceService(source).PlanRemoteMigration(context.Background(), 101, planOptions(target))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if findCheck(plan, "storage").Status != "fail" {
		t.Fatalf("storage = %+v, want fail for insufficient capacity", findCheck(plan, "storage"))
	}
}

func TestPlanRemoteMigrationOnbootWarning(t *testing.T) {
	source := &migrationConfigBackend{fakeBackend: migrationSourceBackend(), config: map[string]any{
		"rootfs": "local-lvm:vm-101-disk-0,size=8G",
		"onboot": "1",
	}}
	target := migrationTargetBackend()

	plan, err := sourceService(source).PlanRemoteMigration(context.Background(), 101, planOptions(target))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if findCheck(plan, "onboot").Status != "warning" {
		t.Fatalf("onboot = %+v", findCheck(plan, "onboot"))
	}
}

// Without a native config backend the dependent checks degrade to unknown
// instead of pretending safety.
func TestPlanRemoteMigrationWithoutConfigBackend(t *testing.T) {
	source := migrationSourceBackend()
	target := migrationTargetBackend()

	plan, err := sourceService(source).PlanRemoteMigration(context.Background(), 101, planOptions(target))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, check := range []string{"storage", "network", "onboot", "mount"} {
		if findCheck(plan, check).Status != "unknown" {
			t.Fatalf("%s = %+v, want unknown", check, findCheck(plan, check))
		}
	}
}

// Device passthrough is surfaced as a warning.
func TestPlanRemoteMigrationDevicePassthroughWarning(t *testing.T) {
	source := &migrationConfigBackend{fakeBackend: migrationSourceBackend(), config: map[string]any{
		"rootfs": "local-lvm:vm-101-disk-0,size=8G",
		"dev0":   "/dev/dri/renderD128",
	}}
	target := migrationTargetBackend()

	plan, err := sourceService(source).PlanRemoteMigration(context.Background(), 101, planOptions(target))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	mount := findCheck(plan, "mount")
	if mount.Status != "warning" || findCheckMessageContains(plan, "mount", "dev0") == false {
		t.Fatalf("mount = %+v", mount)
	}
}

func findCheck(plan output.MigrationPlan, check string) output.MigrationCheckRow {
	for _, row := range plan.Checks {
		if row.Check == check {
			return row
		}
	}
	return output.MigrationCheckRow{}
}

func findCheckMessageContains(plan output.MigrationPlan, check, substr string) bool {
	row := findCheck(plan, check)
	return strings.Contains(row.Message, substr)
}
