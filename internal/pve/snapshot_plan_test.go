package pve

import (
	"context"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"testing"
)

func TestSnapshotPlanMakesNoMutation(t *testing.T) {
	g := &fakeGuest{row: output.GuestRow{Kind: "vm", VMID: 100, Node: "n", Status: "stopped"}, snapshots: []output.SnapshotRow{{Name: "safe"}}}
	b := &fakeBackend{vms: map[string]map[int]*fakeGuest{"n": {100: g}}}
	s := NewVMService(b, TaskRunner{}, nil, false)
	plan, e := s.PlanSnapshot(context.Background(), 100, "n", "safe", "rollback")
	if e != nil || plan.Node != "n" || g.rollbackSnapshot != "" || g.deletedSnapshot != "" {
		t.Fatalf("%+v %v", plan, e)
	}
	g.row.Status = "running"
	if _, e = s.PlanSnapshot(context.Background(), 100, "n", "safe", "rollback"); e == nil {
		t.Fatal("running VM rollback accepted")
	}
	if _, e = s.PlanSnapshot(context.Background(), 100, "n", "missing", "delete"); e == nil {
		t.Fatal("missing snapshot accepted")
	}
}
