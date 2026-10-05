package pve

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

func TestGuestAggregateServiceListFiltersAndSorts(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve2"}, {Name: "pve1"}},
		vmRows: map[string][]output.GuestRow{
			"pve2": {{Kind: "vm", VMID: 200, Name: "app-vm", Node: "pve2", Status: "running"}},
			"pve1": {{Kind: "vm", VMID: 100, Name: "db-vm", Node: "pve1", Status: "stopped"}},
		},
		lxcRows: map[string][]output.GuestRow{
			"pve2": {{Kind: "lxc", VMID: 50, Name: "web-lxc", Node: "pve2", Status: "running"}},
			"pve1": {{Kind: "lxc", VMID: 100, Name: "db-lxc", Node: "pve1", Status: "running"}},
		},
	}
	svc := NewGuestAggregateService(backend, nil, false)

	rows, err := svc.List(context.Background(), GuestListOptions{Type: GuestTypeAll})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	assertGuestOrder(t, rows, []string{"pve1/100/lxc", "pve1/100/vm", "pve2/50/lxc", "pve2/200/vm"})

	rows, err = svc.List(context.Background(), GuestListOptions{Type: GuestTypeVM})
	if err != nil {
		t.Fatalf("list vm: %v", err)
	}
	assertGuestOrder(t, rows, []string{"pve1/100/vm", "pve2/200/vm"})

	rows, err = svc.List(context.Background(), GuestListOptions{Type: GuestTypeLXC})
	if err != nil {
		t.Fatalf("list lxc: %v", err)
	}
	assertGuestOrder(t, rows, []string{"pve1/100/lxc", "pve2/50/lxc"})

	rows, err = svc.List(context.Background(), GuestListOptions{Type: GuestTypeAll, Status: " RUNNING "})
	if err != nil {
		t.Fatalf("list running: %v", err)
	}
	assertGuestOrder(t, rows, []string{"pve1/100/lxc", "pve2/50/lxc", "pve2/200/vm"})
}

func TestGuestAggregateServiceListSpecificNodeDoesNotTraverseNodes(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}, {Name: "pve2"}},
		vmRows: map[string][]output.GuestRow{
			"pve2": {{Kind: "vm", VMID: 200, Node: "pve2"}},
		},
	}
	svc := NewGuestAggregateService(backend, nil, false)

	rows, err := svc.List(context.Background(), GuestListOptions{Node: "pve2", Type: GuestTypeVM})
	if err != nil {
		t.Fatalf("list node: %v", err)
	}
	assertGuestOrder(t, rows, []string{"pve2/200/vm"})
	if backend.nodeCalls != 0 {
		t.Fatalf("node calls = %d", backend.nodeCalls)
	}
	if backend.vmListCalls["pve2"] != 1 {
		t.Fatalf("vm list calls = %#v", backend.vmListCalls)
	}
	if len(backend.lxcListCalls) != 0 {
		t.Fatalf("lxc list calls = %#v", backend.lxcListCalls)
	}
}

func TestGuestAggregateServiceGet(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms: map[string]map[int]*fakeGuest{
			"pve1": {
				100: {row: output.GuestRow{Kind: "vm", VMID: 100, Name: "app-vm", Node: "pve1"}},
				300: {row: output.GuestRow{Kind: "vm", VMID: 300, Name: "shared-vm", Node: "pve1"}},
			},
		},
		lxcs: map[string]map[int]*fakeGuest{
			"pve1": {
				200: {row: output.GuestRow{Kind: "lxc", VMID: 200, Name: "app-lxc", Node: "pve1"}},
				300: {row: output.GuestRow{Kind: "lxc", VMID: 300, Name: "shared-lxc", Node: "pve1"}},
			},
		},
	}
	svc := NewGuestAggregateService(backend, nil, false)

	tests := []struct {
		name    string
		vmid    int
		options GuestGetOptions
		want    string
	}{
		{name: "auto vm only", vmid: 100, options: GuestGetOptions{Type: GuestTypeAuto}, want: "pve1/100/vm"},
		{name: "auto lxc only", vmid: 200, options: GuestGetOptions{Type: GuestTypeAuto}, want: "pve1/200/lxc"},
		{name: "type vm", vmid: 300, options: GuestGetOptions{Type: GuestTypeVM}, want: "pve1/300/vm"},
		{name: "type lxc", vmid: 300, options: GuestGetOptions{Type: GuestTypeLXC}, want: "pve1/300/lxc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row, err := svc.Get(context.Background(), tt.vmid, tt.options)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got := guestKey(row); got != tt.want {
				t.Fatalf("row = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestGuestAggregateServiceGetAutoNotFound(t *testing.T) {
	backend := &fakeBackend{nodes: []output.NodeRow{{Name: "pve1"}}}
	svc := NewGuestAggregateService(backend, nil, false)

	_, err := svc.Get(context.Background(), 999, GuestGetOptions{Type: GuestTypeAuto})
	if err == nil || err.Error() != "guest 999 not found" {
		t.Fatalf("error = %v", err)
	}
}

func TestGuestAggregateServiceGetAutoAmbiguous(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string]map[int]*fakeGuest{"pve1": {300: {row: output.GuestRow{Kind: "vm", VMID: 300, Node: "pve1"}}}},
		lxcs:  map[string]map[int]*fakeGuest{"pve1": {300: {row: output.GuestRow{Kind: "lxc", VMID: 300, Node: "pve1"}}}},
	}
	svc := NewGuestAggregateService(backend, nil, false)

	_, err := svc.Get(context.Background(), 300, GuestGetOptions{Type: GuestTypeAuto})
	if err == nil || !strings.Contains(err.Error(), "guest 300 is ambiguous") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseGuestTypes(t *testing.T) {
	if got, err := ParseGuestListType(""); err != nil || got != GuestTypeAll {
		t.Fatalf("list default = %q/%v", got, err)
	}
	if got, err := ParseGuestListType("VM"); err != nil || got != GuestTypeVM {
		t.Fatalf("list vm = %q/%v", got, err)
	}
	if _, err := ParseGuestListType("auto"); err == nil {
		t.Fatal("expected invalid list type")
	}
	if got, err := ParseGuestGetType(""); err != nil || got != GuestTypeAuto {
		t.Fatalf("get default = %q/%v", got, err)
	}
	if got, err := ParseGuestGetType("LXC"); err != nil || got != GuestTypeLXC {
		t.Fatalf("get lxc = %q/%v", got, err)
	}
	if _, err := ParseGuestGetType("all"); err == nil {
		t.Fatal("expected invalid get type")
	}
}

func assertGuestOrder(t *testing.T, rows []output.GuestRow, want []string) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("rows len = %d, want %d: %#v", len(rows), len(want), rows)
	}
	for i, row := range rows {
		if got := guestKey(row); got != want[i] {
			t.Fatalf("row %d = %s, want %s; rows = %#v", i, got, want[i], rows)
		}
	}
}

func guestKey(row output.GuestRow) string {
	return row.Node + "/" + strconv.FormatUint(row.VMID, 10) + "/" + row.Kind
}
