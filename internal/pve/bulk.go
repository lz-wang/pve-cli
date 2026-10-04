package pve

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lz-wang/pvectl/internal/output"
)

// Bulk lifecycle action names.
const (
	BulkActionStart    = "start"
	BulkActionShutdown = "shutdown"
	BulkActionReboot   = "reboot"
	BulkActionStop     = "stop"
)

// GuestSelector describes which guests a bulk operation targets. At least one
// constraining field (Node, Status, or Tags) must be set so a bare command
// never sweeps the whole cluster.
type GuestSelector struct {
	Node     string
	Type     GuestType
	Status   string
	Tags     []string
	TagMatch string
}

// Validate rejects selectors that would match the whole cluster by default.
func (sel GuestSelector) Validate() error {
	if sel.Node == "" && sel.Status == "" && len(sel.Tags) == 0 {
		return fmt.Errorf("bulk operations require at least one of --node, --status, or --tag to limit the scope")
	}
	return nil
}

// BulkService resolves guest selections for bulk operations.
type BulkService struct {
	backend GuestBackend
	tasks   TaskRunner
	logger  *slog.Logger
	verbose bool
}

func NewBulkService(backend GuestBackend, tasks TaskRunner, logger *slog.Logger, verbose bool) *BulkService {
	return &BulkService{backend: backend, tasks: tasks, logger: logger, verbose: verbose}
}

// Select resolves the selector to matching guest rows across all nodes.
// Selection tolerates per-node failures as long as one node answers.
func (s *BulkService) Select(ctx context.Context, selector GuestSelector) ([]output.GuestRow, error) {
	if err := selector.Validate(); err != nil {
		return nil, err
	}
	tagMatch, err := ParseTagMatch(selector.TagMatch)
	if err != nil {
		return nil, err
	}
	guestType := selector.Type
	if guestType == "" {
		guestType = GuestTypeAll
	}
	if _, err := ParseGuestListType(string(guestType)); err != nil {
		return nil, err
	}

	rows, err := NewGuestAggregateService(s.backend, s.logger, s.verbose).List(ctx, GuestListOptions{
		Node:     selector.Node,
		Type:     guestType,
		Status:   selector.Status,
		Tags:     selector.Tags,
		TagMatch: tagMatch,
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ParseBulkAction validates a bulk lifecycle action name.
func ParseBulkAction(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case BulkActionStart:
		return BulkActionStart, nil
	case BulkActionShutdown:
		return BulkActionShutdown, nil
	case BulkActionReboot:
		return BulkActionReboot, nil
	case BulkActionStop:
		return BulkActionStop, nil
	default:
		return "", fmt.Errorf("invalid bulk action %q, expected start, shutdown, reboot, or stop", value)
	}
}
