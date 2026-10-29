package pve

import (
	"context"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"testing"
)

type powerFake struct {
	*fakeBackend
	called bool
}

func (b *powerFake) PowerNode(context.Context, string, string) error { b.called = true; return nil }
func TestNodePowerPreflight(t *testing.T) {
	b := &powerFake{fakeBackend: &fakeBackend{nodeDetails: map[string]output.NodeDetail{"n": {Name: "n", Status: "online"}}, vmRows: map[string][]output.GuestRow{"n": {{Kind: "vm", Status: "running"}}}}}
	s := NewNodeService(b)
	plan, e := s.PlanPower(context.Background(), "n", "reboot")
	if e != nil || plan.RunningVM != 1 || b.called {
		t.Fatalf("%+v %v", plan, e)
	}
	r, e := s.Power(context.Background(), "n", "reboot")
	if e != nil || r.Status != "submitted" || !b.called {
		t.Fatalf("%+v %v", r, e)
	}
}
