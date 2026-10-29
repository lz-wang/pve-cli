package pve

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrAmbiguous        = errors.New("ambiguous resource")
	ErrPermissionDenied = errors.New("permission denied")
	ErrUnavailable      = errors.New("unavailable")
	ErrConflict         = errors.New("conflict")
	ErrTaskFailed       = errors.New("task failed")
	ErrTaskTimeout      = errors.New("task outcome unknown")
	ErrInvalid          = errors.New("invalid arguments or configuration")
)

// ClassifyError preserves typed errors and classifies SDK transport responses.
func ClassifyError(err error) error {
	if err == nil {
		return nil
	}
	for _, kind := range []error{ErrNotFound, ErrAmbiguous, ErrPermissionDenied, ErrUnavailable, ErrConflict, ErrTaskFailed, ErrTaskTimeout, ErrInvalid} {
		if errors.Is(err, kind) {
			return err
		}
	}
	var network net.Error
	msg := strings.ToLower(err.Error())
	kind := ErrUnavailable
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		kind = ErrUnavailable
	case errors.As(err, &network):
		kind = ErrUnavailable
	case strings.Contains(msg, "401"), strings.Contains(msg, "403"), strings.Contains(msg, "permission"), strings.Contains(msg, "unauthorized"):
		kind = ErrPermissionDenied
	case strings.Contains(msg, "404"), strings.Contains(msg, "not found"), strings.Contains(msg, "does not exist"):
		kind = ErrNotFound
	case strings.Contains(msg, "409"):
		kind = ErrConflict
	}
	return fmt.Errorf("%w: %w", kind, err)
}
