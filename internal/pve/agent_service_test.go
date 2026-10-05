package pve

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

func TestAgentServiceResolvesNodeWhenOmitted(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}, {Name: "pve2"}},
		vms: map[string]map[int]*fakeGuest{
			"pve2": {100: {row: output.GuestRow{Kind: "vm", VMID: 100, Node: "pve2", Name: "web"}}},
		},
		agentErrs: map[string]error{
			"ping/pve1":    errors.New("wrong node"),
			"network/pve1": errors.New("wrong node"),
			"exec/pve1":    errors.New("wrong node"),
		},
		agentNetwork: []output.AgentNetworkRow{{Name: "eth0"}},
	}
	svc := NewAgentService(backend, nil, false)

	// ping must land on pve2, where the VM actually lives; the pve1 error
	// proves the resolved node, not the caller, chose the target.
	if err := svc.Ping(context.Background(), 100, ""); err != nil {
		t.Fatalf("ping: %v", err)
	}

	rows, err := svc.Network(context.Background(), 100, "")
	if err != nil {
		t.Fatalf("network: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "eth0" {
		t.Fatalf("rows = %#v", rows)
	}

	if _, err := svc.Exec(context.Background(), 100, "", AgentExecOptions{Command: []string{"/usr/bin/uname"}}); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func TestAgentServiceRespectsExplicitNode(t *testing.T) {
	backend := &fakeBackend{
		nodes:     []output.NodeRow{{Name: "pve1"}},
		agentErrs: map[string]error{"ping/pveX": errors.New("used as given")},
	}
	svc := NewAgentService(backend, nil, false)

	err := svc.Ping(context.Background(), 100, "pveX")
	if err == nil || !strings.Contains(err.Error(), "used as given") {
		t.Fatalf("explicit node must be passed through unchanged, err = %v", err)
	}
}

func TestAgentServiceReportsMissingVM(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
	}
	svc := NewAgentService(backend, nil, false)

	if _, err := svc.Network(context.Background(), 999, ""); err == nil {
		t.Fatal("expected not-found error for missing vm")
	}
}
