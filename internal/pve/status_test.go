package pve

import (
	"context"
	"errors"
	"testing"

	"github.com/lz-wang/pvectl/internal/output"
)

func TestStatusServiceReportSummaries(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{
			{Name: "pve2", Status: "online"},
			{Name: "pve1", Status: "online"},
		},
		vmRows: map[string][]output.GuestRow{
			"pve1": {
				{Kind: "vm", VMID: 100, Node: "pve1", Status: "running"},
				{Kind: "vm", VMID: 101, Node: "pve1", Status: "stopped"},
			},
			"pve2": {{Kind: "vm", VMID: 102, Node: "pve2", Status: "running"}},
		},
		lxcRows: map[string][]output.GuestRow{
			"pve1": {{Kind: "lxc", VMID: 200, Node: "pve1", Status: "running"}},
		},
		storageRows: map[string][]output.StorageRow{
			"pve1": {
				{Node: "pve1", Storage: "local", Type: "dir", Active: true, Content: "iso,vztmpl"},
				{Node: "pve1", Storage: "backup", Type: "dir", Active: true, Content: "backup"},
			},
		},
		backupRows: map[string]map[string][]output.BackupRow{
			"pve1": {
				"backup": {
					{Node: "pve1", Storage: "backup", Kind: "vm", VMID: 100, CTime: 100},
					{Node: "pve1", Storage: "backup", Kind: "lxc", VMID: 200, CTime: 500},
				},
			},
		},
	}
	svc := NewStatusService(backend)

	report, err := svc.Report(context.Background())
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if report.Nodes.Total != 2 || report.Nodes.Online != 2 || report.Nodes.Offline != 0 {
		t.Fatalf("nodes = %#v", report.Nodes)
	}
	if report.Nodes.Rows[0].Name != "pve1" {
		t.Fatalf("node rows not sorted: %#v", report.Nodes.Rows)
	}
	if report.Guests != (output.GuestSummary{Total: 4, Running: 3, Stopped: 1, VM: 3, LXC: 1}) {
		t.Fatalf("guests = %#v", report.Guests)
	}
	if report.Storages.Total != 2 || report.Storages.Active != 2 {
		t.Fatalf("storages = %#v", report.Storages)
	}
	if report.Backups.Count != 2 || report.Backups.LatestCtime != 500 {
		t.Fatalf("backups = %#v", report.Backups)
	}
	if len(report.Issues) != 0 {
		t.Fatalf("issues = %#v", report.Issues)
	}
}

func TestStatusServiceReportPartialFailures(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{
			{Name: "pve1", Status: "online"},
			{Name: "pve2", Status: "offline"},
		},
		vmErrs: map[string]error{"pve1": errors.New("timeout")},
		lxcRows: map[string][]output.GuestRow{
			"pve1": {{Kind: "lxc", VMID: 200, Node: "pve1", Status: "running"}},
			"pve2": {{Kind: "lxc", VMID: 201, Node: "pve2", Status: "stopped"}},
		},
		storageRows: map[string][]output.StorageRow{
			"pve1": {{Node: "pve1", Storage: "backup", Active: true, Content: "backup"}},
			"pve2": {{Node: "pve2", Storage: "backup", Active: true, Content: "backup"}},
		},
		storageErrs: map[string]error{"pve2": errors.New("timeout")},
		backupErrs:  map[string]error{"pve1/backup": errors.New("timeout")},
		backupRows: map[string]map[string][]output.BackupRow{
			"pve1": {"backup": {{Node: "pve1", Storage: "backup", CTime: 10}}},
		},
	}
	svc := NewStatusService(backend)

	report, err := svc.Report(context.Background())
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if report.Guests.Total != 2 || report.Guests.Running != 1 || report.Guests.Stopped != 1 {
		t.Fatalf("guests = %#v", report.Guests)
	}
	if report.Storages.Total != 1 {
		t.Fatalf("storages = %#v", report.Storages)
	}
	if report.Backups.Count != 0 {
		t.Fatalf("backups = %#v", report.Backups)
	}

	components := map[string]int{}
	for _, issue := range report.Issues {
		components[issue.Component]++
	}
	if components["guest"] != 1 || components["storage"] != 1 || components["backup"] != 1 {
		t.Fatalf("issues = %#v", report.Issues)
	}
}

func TestStatusServiceFailsWhenNodesUnreachable(t *testing.T) {
	backend := &failingNodesBackend{}
	svc := NewStatusService(backend)

	if _, err := svc.Report(context.Background()); err == nil {
		t.Fatal("expected node listing failure")
	}
}

type failingNodesBackend struct {
	fakeBackend
}

func (b *failingNodesBackend) Nodes(context.Context) ([]output.NodeRow, error) {
	return nil, errors.New("connection refused")
}
