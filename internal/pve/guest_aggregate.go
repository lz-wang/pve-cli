package pve

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/lz-wang/pvectl/internal/output"
)

type GuestType string

const (
	GuestTypeAuto GuestType = "auto"
	GuestTypeAll  GuestType = "all"
	GuestTypeVM   GuestType = "vm"
	GuestTypeLXC  GuestType = "lxc"
)

type GuestListOptions struct {
	Node   string
	Type   GuestType
	Status string
}

type GuestGetOptions struct {
	Node string
	Type GuestType
}

type GuestAggregateService struct {
	backend GuestBackend
	logger  *slog.Logger
	verbose bool
}

func NewGuestAggregateService(backend GuestBackend, logger *slog.Logger, verbose bool) *GuestAggregateService {
	return &GuestAggregateService{backend: backend, logger: logger, verbose: verbose}
}

func ParseGuestListType(value string) (GuestType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(GuestTypeAll):
		return GuestTypeAll, nil
	case string(GuestTypeVM):
		return GuestTypeVM, nil
	case string(GuestTypeLXC):
		return GuestTypeLXC, nil
	default:
		return "", fmt.Errorf("invalid guest type %q, expected all, vm, or lxc", value)
	}
}

func ParseGuestGetType(value string) (GuestType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(GuestTypeAuto):
		return GuestTypeAuto, nil
	case string(GuestTypeVM):
		return GuestTypeVM, nil
	case string(GuestTypeLXC):
		return GuestTypeLXC, nil
	default:
		return "", fmt.Errorf("invalid guest type %q, expected auto, vm, or lxc", value)
	}
}

func (s *GuestAggregateService) List(ctx context.Context, options GuestListOptions) ([]output.GuestRow, error) {
	guestType := options.Type
	if guestType == "" {
		guestType = GuestTypeAll
	}

	includeVM, includeLXC, err := guestTypeIncludes(guestType)
	if err != nil {
		return nil, err
	}

	var rows []output.GuestRow
	if options.Node != "" {
		rows, err = s.listOnNode(ctx, options.Node, includeVM, includeLXC)
		if err != nil {
			return nil, err
		}
		return sortGuestRows(filterGuestRows(rows, options.Status)), nil
	}

	nodes, err := s.backend.Nodes(ctx)
	if err != nil {
		return nil, err
	}

	successes := 0
	var firstErr error
	for _, node := range nodes {
		if node.Name == "" {
			continue
		}

		nodeRows, err := s.listOnNode(ctx, node.Name, includeVM, includeLXC)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			s.debug("skip node", "node", node.Name, "error", err)
			continue
		}

		successes++
		rows = append(rows, nodeRows...)
	}

	if successes == 0 && firstErr != nil {
		return nil, fmt.Errorf("list guests: no nodes could be queried: %w", firstErr)
	}

	return sortGuestRows(filterGuestRows(rows, options.Status)), nil
}

func (s *GuestAggregateService) Get(ctx context.Context, vmid int, options GuestGetOptions) (output.GuestRow, error) {
	if vmid <= 0 {
		return output.GuestRow{}, fmt.Errorf("invalid vmid %d", vmid)
	}

	guestType := options.Type
	if guestType == "" {
		guestType = GuestTypeAuto
	}

	switch guestType {
	case GuestTypeVM:
		return NewVMService(s.backend, TaskRunner{}, s.logger, s.verbose).Get(ctx, vmid, options.Node)
	case GuestTypeLXC:
		return NewLXCService(s.backend, TaskRunner{}, s.logger, s.verbose).Get(ctx, vmid, options.Node)
	case GuestTypeAuto:
		return s.getAuto(ctx, vmid, options.Node)
	default:
		return output.GuestRow{}, fmt.Errorf("invalid guest type %q", guestType)
	}
}

func (s *GuestAggregateService) listOnNode(ctx context.Context, node string, includeVM, includeLXC bool) ([]output.GuestRow, error) {
	var rows []output.GuestRow
	if includeVM {
		vmRows, err := s.backend.VMs(ctx, node)
		if err != nil {
			return nil, fmt.Errorf("list vm on node %s: %w", node, err)
		}
		rows = append(rows, vmRows...)
	}
	if includeLXC {
		lxcRows, err := s.backend.LXCs(ctx, node)
		if err != nil {
			return nil, fmt.Errorf("list lxc on node %s: %w", node, err)
		}
		rows = append(rows, lxcRows...)
	}
	return rows, nil
}

func (s *GuestAggregateService) getAuto(ctx context.Context, vmid int, node string) (output.GuestRow, error) {
	found := make([]output.GuestRow, 0, 2)
	var firstErr error

	if row, err := NewVMService(s.backend, TaskRunner{}, s.logger, s.verbose).Get(ctx, vmid, node); err == nil {
		found = append(found, row)
	} else if !isNotFoundError(err) && firstErr == nil {
		firstErr = err
	}

	if row, err := NewLXCService(s.backend, TaskRunner{}, s.logger, s.verbose).Get(ctx, vmid, node); err == nil {
		found = append(found, row)
	} else if !isNotFoundError(err) && firstErr == nil {
		firstErr = err
	}

	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		if firstErr != nil {
			return output.GuestRow{}, firstErr
		}
		return output.GuestRow{}, fmt.Errorf("guest %d not found", vmid)
	default:
		return output.GuestRow{}, fmt.Errorf("guest %d is ambiguous: found both vm and lxc, use --type vm or --type lxc", vmid)
	}
}

func guestTypeIncludes(guestType GuestType) (bool, bool, error) {
	switch guestType {
	case GuestTypeAll:
		return true, true, nil
	case GuestTypeVM:
		return true, false, nil
	case GuestTypeLXC:
		return false, true, nil
	default:
		return false, false, fmt.Errorf("invalid guest type %q", guestType)
	}
}

func filterGuestRows(rows []output.GuestRow, status string) []output.GuestRow {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return rows
	}

	out := rows[:0]
	for _, row := range rows {
		if strings.ToLower(strings.TrimSpace(row.Status)) == status {
			out = append(out, row)
		}
	}
	return out
}

func sortGuestRows(rows []output.GuestRow) []output.GuestRow {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Node != rows[j].Node {
			return rows[i].Node < rows[j].Node
		}
		if rows[i].VMID != rows[j].VMID {
			return rows[i].VMID < rows[j].VMID
		}
		return rows[i].Kind < rows[j].Kind
	})
	return rows
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNotFound) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

func (s *GuestAggregateService) debug(msg string, args ...any) {
	if s.verbose && s.logger != nil {
		s.logger.Debug(msg, args...)
	}
}
