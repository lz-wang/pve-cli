package pve

import (
	"context"
	"testing"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

func networkTestBackend() *fakeBackend {
	return &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		networkRows: map[string][]output.NetworkRow{
			"pve1": {
				{Node: "pve1", Name: "vmbr0", Type: "bridge", Active: true, Autostart: true, CIDR: "192.168.2.2/24"},
				{Node: "pve1", Name: "bond0", Type: "bond", Active: false, BondSlaves: "enp1s0 enp2s0"},
				{Node: "pve1", Name: "vmbr9", Type: "bridge", Active: false},
			},
		},
	}
}

func TestNetworkServiceListFilters(t *testing.T) {
	svc := NewNetworkService(networkTestBackend(), nil, false)

	rows, err := svc.List(context.Background(), "pve1", NetworkListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %#v", rows)
	}

	rows, err = svc.List(context.Background(), "pve1", NetworkListOptions{Type: "bridge", Active: true})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "vmbr0" {
		t.Fatalf("filtered rows = %#v", rows)
	}
}

func TestNetworkServiceListAggregatesNodes(t *testing.T) {
	backend := networkTestBackend()
	backend.nodes = append(backend.nodes, output.NodeRow{Name: "pve2"})
	backend.networkRows["pve2"] = []output.NetworkRow{{Node: "pve2", Name: "vmbr0", Type: "bridge"}}
	svc := NewNetworkService(backend, nil, false)

	rows, err := svc.List(context.Background(), "", NetworkListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 4 || rows[0].Node != "pve1" || rows[3].Node != "pve2" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestNetworkServiceGet(t *testing.T) {
	svc := NewNetworkService(networkTestBackend(), nil, false)

	row, err := svc.Get(context.Background(), "pve1", "bond0")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.BondSlaves != "enp1s0 enp2s0" {
		t.Fatalf("row = %#v", row)
	}

	if _, err := svc.Get(context.Background(), "pve1", "missing"); err == nil {
		t.Fatal("expected not found error")
	}
	if _, err := svc.Get(context.Background(), "pve1", " "); err == nil {
		t.Fatal("expected empty iface error")
	}
}
