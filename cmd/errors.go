package cmd

import (
	"errors"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/urfave/cli/v2"
	"strings"
)

// ExitCode is the process-level v3 error contract.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit cli.ExitCoder
	if errors.As(err, &exit) {
		if exit.ExitCode() == 3 {
			return 2
		}
		return exit.ExitCode()
	}
	switch {
	case errors.Is(err, pve.ErrInvalid):
		return 2
	case errors.Is(err, pve.ErrTaskTimeout):
		return 5
	case errors.Is(err, pve.ErrNotFound), errors.Is(err, pve.ErrAmbiguous), errors.Is(err, pve.ErrConflict):
		return 4
	case errors.Is(err, pve.ErrPermissionDenied), errors.Is(err, pve.ErrUnavailable):
		return 3
	}
	msg := err.Error()
	for _, prefix := range []string{"config error:", "invalid ", "expected ", "flag provided", "Required flag", "at least one", "empty key", " is required", " must ", " cannot ", " accepts ", "choose ", "not set", "requires a", "non-interactive", "unknown command", "No help topic", "--backup-tag and"} {
		if strings.Contains(msg, prefix) {
			return 2
		}
	}
	return 1
}
