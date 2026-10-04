package pve

import (
	"context"
	"testing"

	"github.com/lz-wang/pvectl/internal/output"
)

func TestGuestSelectorValidateRequiresScope(t *testing.T) {
	if err := (GuestSelector{}).Validate(); err == nil {
		t.Fatal("expected empty selector to be rejected")
	}
	for _, selector := range []GuestSelector{
		{Node: "pve1"},
		{Status: "running"},
		{Tags: []string{"infra"}},
	} {
		if err := selector.Validate(); err != nil {
			t.Fatalf("selector %#v: %v", selector, err)
		}
	}
}

func TestBulkServiceSelectFiltersAndSorts(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}, {Name: "pve2"}},
		vmRows: map[string][]output.GuestRow{
			"pve1": {{Kind: "vm", VMID: 100, Name: "debian", Node: "pve1", Status: "running", Tags: "infra"}},
			"pve2": {{Kind: "vm", VMID: 102, Name: "play", Node: "pve2", Status: "running", Tags: "sandbox"}},
		},
		lxcRows: map[string][]output.GuestRow{
			"pve1": {{Kind: "lxc", VMID: 200, Name: "agh", Node: "pve1", Status: "running", Tags: "infra;production"}},
		},
	}
	svc := NewBulkService(backend, TaskRunner{}, nil, false)

	rows, err := svc.Select(context.Background(), GuestSelector{Tags: []string{"infra"}})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(rows) != 2 || rows[0].VMID != 100 || rows[1].VMID != 200 {
		t.Fatalf("rows = %#v", rows)
	}

	rows, err = svc.Select(context.Background(), GuestSelector{Tags: []string{"infra"}, Type: GuestTypeLXC})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(rows) != 1 || rows[0].Kind != "lxc" {
		t.Fatalf("type filtered rows = %#v", rows)
	}

	if _, err := svc.Select(context.Background(), GuestSelector{}); err == nil {
		t.Fatal("expected empty selector error")
	}

	if _, err := svc.Select(context.Background(), GuestSelector{Tags: []string{"infra"}, TagMatch: "bogus"}); err == nil {
		t.Fatal("expected invalid tag match error")
	}
}

func TestParseBulkAction(t *testing.T) {
	for _, value := range []string{"start", "shutdown", "reboot", "stop"} {
		if _, err := ParseBulkAction(value); err != nil {
			t.Fatalf("ParseBulkAction(%q): %v", value, err)
		}
	}
	if _, err := ParseBulkAction("delete"); err == nil {
		t.Fatal("expected invalid bulk action error")
	}
}

func TestBulkServiceExecuteRowsReturnsPerGuestResults(t *testing.T) {
	failing := &fakeGuest{row: output.GuestRow{Kind: "vm", VMID: 100, Node: "pve1"}, actionErr: errBulkBoom{}}
	ok := &fakeGuest{row: output.GuestRow{Kind: "vm", VMID: 101, Node: "pve1"}, task: &fakeTask{upid: "UPID:pve1:bulk101"}}
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string]map[int]*fakeGuest{"pve1": {100: failing, 101: ok}},
	}
	svc := NewBulkService(backend, TaskRunner{}, nil, false)

	results, err := svc.ExecuteRows(context.Background(), BulkActionShutdown, []output.GuestRow{
		{Kind: "vm", VMID: 100, Node: "pve1", Name: "a"},
		{Kind: "vm", VMID: 101, Node: "pve1", Name: "b"},
	}, BulkExecuteOptions{})
	if err == nil {
		t.Fatal("expected aggregated failure")
	}
	if len(results) != 2 {
		t.Fatalf("results = %#v", results)
	}
	if results[0].Status != BulkResultStatusError || results[0].Error == "" {
		t.Fatalf("result[0] = %#v", results[0])
	}
	if results[1].Status != BulkResultStatusOK || results[1].Task != "UPID:pve1:bulk101" {
		t.Fatalf("result[1] = %#v", results[1])
	}
}

type errBulkBoom struct{}

func (errBulkBoom) Error() string { return "boom" }
