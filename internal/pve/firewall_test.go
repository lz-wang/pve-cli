package pve

import (
	"context"
	"testing"

	"github.com/lz-wang/pvectl/internal/output"
)

func firewallTestBackend() *fakeBackend {
	return &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		firewallStatus: map[string]output.FirewallStatusRow{
			"node/pve1/0": {Scope: "node", Node: "pve1", Enabled: true},
			"vm/pve1/100": {Scope: "vm", Node: "pve1", VMID: 100, Enabled: false},
		},
		firewallRules: map[string][]output.FirewallRuleRow{
			"node/pve1/0": {
				{Scope: "node", Node: "pve1", Position: 1, Enabled: true, Direction: "in", Action: "ACCEPT", DestPort: "22", Protocol: "tcp"},
			},
			"vm/pve1/100": {
				{Scope: "vm", Node: "pve1", VMID: 100, Position: 1, Enabled: true, Direction: "in", Action: "ACCEPT", Source: "192.168.2.0/24"},
			},
		},
	}
}

func TestParseFirewallScope(t *testing.T) {
	scope, err := ParseFirewallScope("pve1", "", 0)
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	if scope.Type != FirewallScopeNode || scope.Node != "pve1" {
		t.Fatalf("scope = %#v", scope)
	}

	scope, err = ParseFirewallScope("pve1", "lxc", 200)
	if err != nil {
		t.Fatalf("scope: %v", err)
	}
	if scope.Type != FirewallScopeLXC || scope.VMID != 200 {
		t.Fatalf("scope = %#v", scope)
	}

	if _, err := ParseFirewallScope("", "node", 0); err == nil {
		t.Fatal("expected missing node error")
	}
	if _, err := ParseFirewallScope("pve1", "vm", 0); err == nil {
		t.Fatal("expected missing vmid error")
	}
	if _, err := ParseFirewallScope("pve1", "bogus", 0); err == nil {
		t.Fatal("expected invalid scope error")
	}

	// vm/lxc scopes keep the node empty so the service locates the guest.
	scope, err = ParseFirewallScope("", "vm", 100)
	if err != nil {
		t.Fatalf("scope without node: %v", err)
	}
	if scope.Node != "" || scope.VMID != 100 {
		t.Fatalf("scope = %#v", scope)
	}
}

func TestFirewallBackendStatusAndRules(t *testing.T) {
	backend := firewallTestBackend()

	status, err := backend.FirewallStatus(context.Background(), FirewallScope{Node: "pve1", Type: FirewallScopeNode})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Enabled {
		t.Fatalf("status = %#v", status)
	}

	rules, err := backend.FirewallRules(context.Background(), FirewallScope{Node: "pve1", Type: FirewallScopeVM, VMID: 100})
	if err != nil {
		t.Fatalf("rules: %v", err)
	}
	if len(rules) != 1 || rules[0].VMID != 100 || rules[0].Scope != "vm" {
		t.Fatalf("rules = %#v", rules)
	}
}

func TestFirewallServiceLocatesGuestNode(t *testing.T) {
	backend := firewallTestBackend()
	backend.nodes = []output.NodeRow{{Name: "pve1"}, {Name: "pve2"}}
	backend.vms = map[string]map[int]*fakeGuest{
		"pve2": {100: {row: output.GuestRow{Kind: "vm", VMID: 100, Node: "pve2"}}},
	}
	backend.firewallStatus["vm/pve2/100"] = output.FirewallStatusRow{Scope: "vm", Node: "pve2", VMID: 100, Enabled: true}

	svc := NewFirewallService(backend, nil, false)
	status, err := svc.Status(context.Background(), FirewallScope{Type: FirewallScopeVM, VMID: 100})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Node != "pve2" || status.VMID != 100 || !status.Enabled {
		t.Fatalf("status = %#v", status)
	}
}
