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
