package pve

import (
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"testing"
)

func TestGuestQueryMissingLast(t *testing.T) {
	rows := []output.GuestRow{{VMID: 3, Name: "Debian", MaxMem: 0}, {VMID: 2, Name: "debian2", MaxMem: 100}, {VMID: 1, Name: "debian1", MaxMem: 200}}
	got, err := QueryGuests(rows, "DEBIAN", "", "memory", true)
	if err != nil || len(got) != 3 || got[0].VMID != 1 || got[2].VMID != 3 {
		t.Fatalf("%+v %v", got, err)
	}
}
