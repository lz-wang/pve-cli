package pve

import (
	"context"
	"errors"
	"fmt"
	"testing"

	proxmox "github.com/luthermonson/go-proxmox"
	"github.com/lz-wang/pve-cli/v3/internal/output"
)

type inventoryBackend struct {
	*fakeBackend
	rows []output.GuestRow
	err  error
}

func (b inventoryBackend) ClusterGuests(context.Context) ([]output.GuestRow, error) {
	return b.rows, b.err
}
func TestResolverCluster(t *testing.T) {
	rows := []output.GuestRow{{Kind: "vm", VMID: 100, Node: "a", Name: "same"}, {Kind: "vm", VMID: 101, Node: "b", Name: "same"}}
	s := NewVMService(inventoryBackend{rows: rows}, TaskRunner{}, nil, false)
	r, err := s.Resolve(context.Background(), "100", "", false)
	if err != nil || r.Node != "a" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err = s.Resolve(context.Background(), "same", "", false); !errors.Is(err, ErrAmbiguous) {
		t.Fatal(err)
	}
	if _, err = s.Resolve(context.Background(), "999", "", false); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// HTTP statuses are classified structurally at the transport; SDK auth
	// failures surface as ErrNotAuthorized and map to permission denied.
	s = NewVMService(inventoryBackend{err: fmt.Errorf("cluster inventory: %w", proxmox.ErrNotAuthorized)}, TaskRunner{}, nil, false)
	if _, err = s.Resolve(context.Background(), "100", "", false); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal(err)
	}
}
