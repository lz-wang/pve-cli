package pve

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

// GuestRef uniquely identifies a VM or container.
type GuestRef struct {
	Kind string
	VMID int
	Node string
	Name string
}

// ClusterGuestBackend is the cluster inventory capability used by resolution.
type ClusterGuestBackend interface {
	ClusterGuests(context.Context) ([]output.GuestRow, error)
}

func (b *ProxmoxBackend) ClusterGuests(ctx context.Context) ([]output.GuestRow, error) {
	var rows []struct {
		Type   string `json:"type"`
		VMID   int    `json:"vmid"`
		Node   string `json:"node"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := b.client.Get(ctx, "/cluster/resources?type=vm", &rows); err != nil {
		return nil, ClassifyError(err)
	}
	result := make([]output.GuestRow, 0, len(rows))
	for _, r := range rows {
		kind := r.Type
		if kind == "qemu" {
			kind = "vm"
		}
		if kind != "vm" && kind != "lxc" {
			continue
		}
		result = append(result, output.GuestRow{Kind: kind, VMID: uint64(r.VMID), Node: r.Node, Name: r.Name, Status: r.Status})
	}
	return result, nil
}

// Resolve locates an exact ID or name, refusing incomplete inventories.
func (s *GuestService) Resolve(ctx context.Context, value, node string, byName bool) (GuestRef, error) {
	id, parseErr := strconv.Atoi(value)
	if !byName && parseErr == nil && id <= 0 {
		return GuestRef{}, fmt.Errorf("%w: invalid vmid", ErrInvalid)
	}
	matches := []output.GuestRow{}
	match := func(row output.GuestRow) bool {
		return row.Kind == s.kind && (node == "" || row.Node == node) && ((!byName && parseErr == nil && int(row.VMID) == id) || ((byName || parseErr != nil) && row.Name == value))
	}
	if cb, ok := s.backend.(ClusterGuestBackend); ok && node == "" {
		rows, err := cb.ClusterGuests(ctx)
		if err != nil {
			return GuestRef{}, ClassifyError(err)
		}
		for _, r := range rows {
			if match(r) {
				matches = append(matches, r)
			}
		}
	} else if !byName && parseErr == nil {
		nodes := []output.NodeRow{{Name: node}}
		if node == "" {
			var err error
			nodes, err = s.backend.Nodes(ctx)
			if err != nil {
				return GuestRef{}, ClassifyError(err)
			}
		}
		for _, n := range nodes {
			guest, err := s.getOnNode(ctx, n.Name, id)
			if err != nil {
				err = ClassifyError(err)
				if errors.Is(err, ErrNotFound) {
					continue
				}
				return GuestRef{}, err
			}
			row := guest.Row()
			if row.Kind == "" {
				row.Kind = s.kind
			}
			if match(row) {
				matches = append(matches, row)
			}
		}
	} else {
		nodes := []output.NodeRow{{Name: node}}
		if node == "" {
			var err error
			nodes, err = s.backend.Nodes(ctx)
			if err != nil {
				return GuestRef{}, ClassifyError(err)
			}
		}
		for _, n := range nodes {
			rows, err := s.listOnNode(ctx, n.Name)
			if err != nil {
				return GuestRef{}, ClassifyError(err)
			}
			for _, r := range rows {
				if match(r) {
					matches = append(matches, r)
				}
			}
		}
	}
	if len(matches) == 0 {
		return GuestRef{}, fmt.Errorf("%w: %s %s", ErrNotFound, s.kind, value)
	}
	if len(matches) > 1 {
		candidates := []string{}
		for _, r := range matches {
			candidates = append(candidates, fmt.Sprintf("%s/%d", r.Node, r.VMID))
		}
		return GuestRef{}, fmt.Errorf("%w: %s: %s", ErrAmbiguous, value, strings.Join(candidates, ", "))
	}
	r := matches[0]
	return GuestRef{Kind: s.kind, VMID: int(r.VMID), Node: r.Node, Name: r.Name}, nil
}
