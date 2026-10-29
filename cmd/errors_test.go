package cmd

import (
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"testing"
)

func TestExitCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{{nil, 0}, {pve.ErrInvalid, 2}, {pve.ErrUnavailable, 3}, {pve.ErrPermissionDenied, 3}, {pve.ErrNotFound, 4}, {pve.ErrAmbiguous, 4}, {pve.ErrTaskFailed, 1}, {pve.ErrTaskTimeout, 5}} {
		if got := ExitCode(tc.err); got != tc.code {
			t.Fatalf("%v: %d", tc.err, got)
		}
		if tc.err != nil && ExitCode(fmt.Errorf("wrapped: %w", tc.err)) != tc.code {
			t.Fatal("lost classification")
		}
	}
}
