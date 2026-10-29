package pve

import (
	"context"
	"errors"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"testing"
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
	s = NewVMService(inventoryBackend{err: errors.New("403 forbidden")}, TaskRunner{}, nil, false)
	if _, err = s.Resolve(context.Background(), "100", "", false); !errors.Is(err, ErrPermissionDenied) {
		t.Fatal(err)
	}
}
