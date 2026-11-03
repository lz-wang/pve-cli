package pve

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"
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

// ClassifyError preserves typed errors (including *APIError from the HTTP
// transport, which already carries its semantic kind) and classifies
// transport-level failures. HTTP status codes are classified by the
// structured transport; bare numbers are never matched in message text.
func ClassifyError(err error) error {
	if err == nil {
		return nil
	}
	for _, kind := range []error{ErrNotFound, ErrAmbiguous, ErrPermissionDenied, ErrUnavailable, ErrConflict, ErrTaskFailed, ErrTaskTimeout, ErrInvalid} {
		if errors.Is(err, kind) {
			return err
		}
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return err
	}
	var network net.Error
	msg := strings.ToLower(err.Error())
	kind := ErrUnavailable
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		kind = ErrUnavailable
	case errors.As(err, &network):
		kind = ErrUnavailable
	case errors.Is(err, proxmox.ErrNotAuthorized):
		kind = ErrPermissionDenied
	case strings.Contains(msg, "permission"), strings.Contains(msg, "unauthorized"), strings.Contains(msg, "not authorized"):
		kind = ErrPermissionDenied
	case strings.Contains(msg, "not found"), strings.Contains(msg, "does not exist"), strings.Contains(msg, "no such "):
		kind = ErrNotFound
	}
	return fmt.Errorf("%w: %w", kind, err)
}
