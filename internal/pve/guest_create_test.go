package pve

import (
	"context"
	"errors"
	"testing"
)

func TestLXCCreateRejectsInvalidBeforeAPI(t *testing.T) {
	// A nil backend makes any accidental API access fail immediately.
	s := NewCreateService(nil, TaskRunner{})
	for _, o := range []LXCCreateOptions{{ID: 201}, {ID: 201, Memory: 1024, Cores: 1, RootFS: "invalid"}, {ID: 201, Memory: 1024, Cores: 1, RootFS: "local:8", Net0: "name=eth0,bridge=x,bridge=y"}} {
		if _, e := s.CreateLXC(context.Background(), o); !errors.Is(e, ErrInvalid) {
			t.Fatalf("expected preflight error: %v", e)
		}
	}
}

func TestVMCreateRejectsPropertyInjection(t *testing.T) {
	s := NewCreateService(nil, TaskRunner{})
	for _, o := range []VMCreateOptions{{DiskSize: "0G"}, {DiskSize: "32G", Memory: 1024, Cores: 1, Storage: "local", Bridge: "vmbr0,firewall=0"}, {DiskSize: "32G", Memory: 1024, Cores: 1, Storage: "local", Bridge: "vmbr0", ISO: "local:a,media=disk"}} {
		if _, e := s.CreateVM(context.Background(), o); !errors.Is(e, ErrInvalid) {
			t.Fatalf("invalid property accepted: %v", e)
		}
	}
}
