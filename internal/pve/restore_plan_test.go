package pve

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

type restoreTestBackend struct {
	*fakeBackend
	config map[string]any
}

func (b *restoreTestBackend) GuestConfig(context.Context, string, string, int) (map[string]any, error) {
	return b.config, nil
}

func newRestoreTestBackend() *restoreTestBackend {
	return &restoreTestBackend{fakeBackend: &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1", Status: "online"}},
		lxcs:  map[string]map[int]*fakeGuest{},
		vms:   map[string]map[int]*fakeGuest{},
		backupRows: map[string]map[string][]output.BackupRow{
			"pve1": {"backup": {{
				Node: "pve1", Storage: "backup", Kind: "lxc", VMID: 101,
				VolID: "backup:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", Size: 8 << 30,
			}}},
		},
		storageRows: map[string][]output.StorageRow{
			"pve1": {{Node: "pve1", Storage: "local-lvm", Type: "lvmthin", Active: true, Enabled: true, Content: "rootdir,images", Avail: 100 << 30}},
		},
		networkRows: map[string][]output.NetworkRow{
			"pve1": {{Node: "pve1", Name: "vmbr0", Type: "bridge", Active: true}},
		},
	}}
}

func restorePlanOptions() RestoreOptions {
	return RestoreOptions{
		Kind:    BackupKindLXC,
		Archive: "backup:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst",
		Node:    "pve1",
		VMID:    201,
		Storage: "local-lvm",
	}
}

func TestPlanRestoreHealthy(t *testing.T) {
	backend := newRestoreTestBackend()
	service := NewRestoreService(backend, TaskRunner{})

	plan, err := service.PlanRestore(context.Background(), restorePlanOptions())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	statuses := map[string]string{}
	for _, row := range plan.Checks {
		statuses[row.Check] = row.Status
	}
	for _, check := range []string{"vmid", "archive", "storage", "network", "onboot", "mac_ip", "mount", "start"} {
		if _, ok := statuses[check]; !ok {
			t.Fatalf("missing check %q: %+v", check, plan.Checks)
		}
	}
	if statuses["vmid"] != "ok" || statuses["archive"] != "ok" || statuses["storage"] != "ok" {
		t.Fatalf("healthy checks failed: %+v", statuses)
	}
	// Archive-internal facts cannot be verified before the restore.
	if statuses["onboot"] != "unknown" || statuses["mac_ip"] != "unknown" || statuses["mount"] != "unknown" {
		t.Fatalf("archive-internal checks must stay unknown: %+v", statuses)
	}
	if statuses["start"] != "ok" {
		t.Fatalf("start = %q, want ok (never started automatically)", statuses["start"])
	}
}

func TestPlanRestoreFailures(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*restoreTestBackend)
		options RestoreOptions
		check   string
	}{
		{
			name: "vmid taken",
			mutate: func(b *restoreTestBackend) {
				b.lxcs = map[string]map[int]*fakeGuest{"pve1": {201: {row: output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve1"}}}}
				b.lxcRows = map[string][]output.GuestRow{"pve1": {{Kind: "lxc", VMID: 201, Node: "pve1"}}}
			},
			options: restorePlanOptions(),
			check:   "vmid",
		},
		{
			name:    "archive missing",
			mutate:  func(b *restoreTestBackend) { b.backupRows = nil },
			options: restorePlanOptions(),
			check:   "archive",
		},
		{
			name: "storage missing",
			mutate: func(b *restoreTestBackend) {
				b.storageRows = map[string][]output.StorageRow{"pve1": {}}
			},
			options: restorePlanOptions(),
			check:   "storage",
		},
		{
			name: "storage capacity insufficient",
			mutate: func(b *restoreTestBackend) {
				b.storageRows["pve1"][0].Avail = 1 << 30
			},
			options: restorePlanOptions(),
			check:   "storage",
		},
		{
			name: "mapped bridge missing",
			mutate: func(b *restoreTestBackend) {
				b.networkRows = map[string][]output.NetworkRow{"pve1": {}}
			},
			options: func() RestoreOptions {
				options := restorePlanOptions()
				options.BridgeMap = map[string]string{"vmbr2": "vmbrX"}
				return options
			}(),
			check: "network",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := newRestoreTestBackend()
			tc.mutate(backend)
			plan, err := NewRestoreService(backend, TaskRunner{}).PlanRestore(context.Background(), tc.options)
			if err != nil {
				t.Fatalf("plan: %v", err)
			}
			if findCheck(plan, tc.check).Status != "fail" {
				t.Fatalf("%s = %+v, want fail", tc.check, findCheck(plan, tc.check))
			}
			if plan.Status != "failed" {
				t.Fatalf("overall = %q", plan.Status)
			}
		})
	}
}

// A failing preflight aborts the restore before any task is submitted.
func TestRestoreAbortsOnFailedPreflight(t *testing.T) {
	backend := newRestoreTestBackend()
	backend.storageRows = map[string][]output.StorageRow{"pve1": {}}
	var out bytes.Buffer
	service := NewRestoreService(backend, TaskRunner{Format: "json", ResultWriter: &out})

	_, err := service.Restore(context.Background(), restorePlanOptions())
	if err == nil || !strings.Contains(err.Error(), "preflight") {
		t.Fatalf("err = %v", err)
	}
	if backend.restoreOptions.Archive != "" {
		t.Fatal("no restore task may be submitted after a failed preflight")
	}
}

// With --wait and a successful task, bridge mappings are applied to the
// restored container's network devices.
func TestRestoreAppliesBridgeMapAfterWait(t *testing.T) {
	backend := newRestoreTestBackend()
	backend.restoreTask = &fakeTask{upid: "UPID:pve1:0001:0000:6839F4A1:vzrestore:201:root@pam:"}
	// The restored container appears after the task completes.
	restored := &fakeGuest{row: output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve1", Status: "stopped"}}
	backend.lxcs["pve1"] = map[int]*fakeGuest{201: restored}
	backend.config = map[string]any{"net0": "name=eth0,bridge=vmbr2"}

	var out bytes.Buffer
	service := NewRestoreService(backend, TaskRunner{Wait: true, WaitTimeout: time.Second, Format: "json", ResultWriter: &out})

	options := restorePlanOptions()
	options.BridgeMap = map[string]string{"vmbr2": "vmbr0"}
	_, err := service.Restore(context.Background(), options)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	submitted := restored.configValues
	if submitted["net0"] != "name=eth0,bridge=vmbr0" {
		t.Fatalf("bridge fixup missing: %+v", submitted)
	}
}

// Unknown source bridges stay informational: only mapped bridges are
// verified; anything else is inside the archive.
func TestPlanRestoreNetworkWithoutMapIsUnknown(t *testing.T) {
	backend := newRestoreTestBackend()
	plan, err := NewRestoreService(backend, TaskRunner{}).PlanRestore(context.Background(), restorePlanOptions())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if findCheck(plan, "network").Status != "unknown" {
		t.Fatalf("network = %+v, want unknown without mapping", findCheck(plan, "network"))
	}
}

func TestPlanRestoreValidations(t *testing.T) {
	backend := newRestoreTestBackend()
	service := NewRestoreService(backend, TaskRunner{})
	if _, err := service.PlanRestore(context.Background(), RestoreOptions{Kind: BackupKindLXC, Archive: "x:y", Node: "pve1", VMID: 0}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if _, err := service.PlanRestore(context.Background(), RestoreOptions{Kind: "bad", Archive: "x:y", Node: "pve1", VMID: 5}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}
