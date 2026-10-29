package pve

import (
	"context"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"testing"
)

type nativeConfigFake struct {
	GuestBackend
	cfg map[string]any
}

func (b nativeConfigFake) GuestConfig(context.Context, string, string, int) (map[string]any, error) {
	return b.cfg, nil
}
func TestNativeConfigRedactsCopy(t *testing.T) {
	cfg := map[string]any{"memory": 4096, "cipassword": "do-not-print"}
	base := &fakeBackend{vms: map[string]map[int]*fakeGuest{"n": {100: {row: output.GuestRow{VMID: 100, Node: "n", Kind: "vm"}}}}}
	s := NewVMService(nativeConfigFake{base, cfg}, TaskRunner{}, nil, false)
	got, e := s.NativeConfig(context.Background(), 100, "n")
	if e != nil || got["memory"] != 4096 || got["cipassword"] != "[REDACTED]" || cfg["cipassword"] != "do-not-print" {
		t.Fatalf("redaction/copy failed: %v", e)
	}
}
