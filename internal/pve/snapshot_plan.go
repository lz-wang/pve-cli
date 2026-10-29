package pve

import (
	"context"
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
)

// PlanSnapshot performs only reads. Mutations repeat it immediately before submission.
func (s *GuestService) PlanSnapshot(ctx context.Context, id int, node, name, action string) (output.SnapshotPlan, error) {
	p := output.SnapshotPlan{Resource: s.kind, VMID: id, Snapshot: name, Action: action}
	if name == "" || name == "current" || (action != "rollback" && action != "delete") {
		return p, fmt.Errorf("%w: invalid snapshot target", ErrInvalid)
	}
	g, err := s.resolve(ctx, id, node)
	if err != nil {
		return p, err
	}
	p.Node = g.Row().Node
	snapshots, err := g.Snapshots(ctx)
	if err != nil {
		return p, ClassifyError(err)
	}
	found := false
	for _, snap := range snapshots {
		if snap.Name == name {
			found = true
			if snap.State != "" {
				return p, fmt.Errorf("%w: snapshot is not ready", ErrConflict)
			}
		}
	}
	if !found {
		return p, fmt.Errorf("%w: snapshot %s", ErrNotFound, name)
	}
	if action == "rollback" && s.kind == "vm" && g.Row().Status != "stopped" {
		return p, fmt.Errorf("%w: stop the VM before rollback", ErrConflict)
	}
	if b, ok := s.backend.(NativeConfigBackend); ok {
		cfg, e := b.GuestConfig(ctx, s.kind, p.Node, id)
		if e != nil {
			return p, e
		}
		if lock, ok := cfg["lock"]; ok && fmt.Sprint(lock) != "" {
			return p, fmt.Errorf("%w: guest is locked", ErrConflict)
		}
	}
	p.Impact = "delete the named snapshot and its recovery point; retain current guest state"
	if action == "rollback" {
		p.Impact = "replace guest configuration and disk state with the named snapshot; newer changes will be lost"
	}
	return p, nil
}
