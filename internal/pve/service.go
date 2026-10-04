package pve

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lz-wang/pvectl/internal/output"
)

type NodeService struct {
	backend NodeBackend
}

func NewNodeService(backend NodeBackend) *NodeService {
	return &NodeService{backend: backend}
}

func (s *NodeService) List(ctx context.Context) ([]output.NodeRow, error) {
	return s.backend.Nodes(ctx)
}

// Get returns detailed status for a single node.
func (s *NodeService) Get(ctx context.Context, name string) (output.NodeDetail, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return output.NodeDetail{}, fmt.Errorf("node is required")
	}
	return s.backend.Node(ctx, name)
}

type GuestService struct {
	kind    string
	backend GuestBackend
	tasks   TaskRunner
	logger  *slog.Logger
	verbose bool
}

func NewVMService(backend GuestBackend, tasks TaskRunner, logger *slog.Logger, verbose bool) *GuestService {
	return &GuestService{kind: "vm", backend: backend, tasks: tasks, logger: logger, verbose: verbose}
}

func NewLXCService(backend GuestBackend, tasks TaskRunner, logger *slog.Logger, verbose bool) *GuestService {
	return &GuestService{kind: "lxc", backend: backend, tasks: tasks, logger: logger, verbose: verbose}
}

func (s *GuestService) List(ctx context.Context, node string) ([]output.GuestRow, error) {
	if node != "" {
		return s.listOnNode(ctx, node)
	}

	nodes, err := s.backend.Nodes(ctx)
	if err != nil {
		return nil, err
	}

	rows := make([]output.GuestRow, 0)
	successes := 0
	var firstErr error
	for _, nodeRow := range nodes {
		if nodeRow.Name == "" {
			continue
		}
		nodeRows, err := s.listOnNode(ctx, nodeRow.Name)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			s.debug("skip node", "node", nodeRow.Name, "error", err)
			continue
		}
		successes++
		rows = append(rows, nodeRows...)
	}

	if successes == 0 && firstErr != nil {
		return nil, fmt.Errorf("list %s: no nodes could be queried: %w", s.kind, firstErr)
	}
	return rows, nil
}

func (s *GuestService) Get(ctx context.Context, vmid int, node string) (output.GuestRow, error) {
	guest, err := s.resolve(ctx, vmid, node)
	if err != nil {
		return output.GuestRow{}, err
	}
	return guest.Row(), nil
}

func (s *GuestService) Start(ctx context.Context, vmid int, node string) error {
	return s.run(ctx, vmid, node, func(guest Guest) (Task, error) {
		return guest.Start(ctx)
	})
}

func (s *GuestService) Shutdown(ctx context.Context, vmid int, node string) error {
	return s.run(ctx, vmid, node, func(guest Guest) (Task, error) {
		return guest.Shutdown(ctx)
	})
}

func (s *GuestService) Stop(ctx context.Context, vmid int, node string) error {
	return s.run(ctx, vmid, node, func(guest Guest) (Task, error) {
		return guest.Stop(ctx)
	})
}

func (s *GuestService) Reboot(ctx context.Context, vmid int, node string) error {
	return s.run(ctx, vmid, node, func(guest Guest) (Task, error) {
		return guest.Reboot(ctx)
	})
}

func (s *GuestService) Clone(ctx context.Context, vmid int, node string, options CloneOptions) (CloneResult, error) {
	if options.Target == "" {
		return CloneResult{}, fmt.Errorf("target node is required")
	}
	if s.kind == "vm" && options.Name == "" {
		return CloneResult{}, fmt.Errorf("name is required")
	}
	if s.kind == "lxc" && options.Hostname == "" {
		return CloneResult{}, fmt.Errorf("hostname is required")
	}

	guest, err := s.resolve(ctx, vmid, node)
	if err != nil {
		return CloneResult{}, err
	}
	result, task, err := guest.Clone(ctx, options)
	if err != nil {
		return CloneResult{}, err
	}
	if err := s.tasks.Handle(ctx, task); err != nil {
		return CloneResult{}, err
	}
	return result, nil
}

func (s *GuestService) Config(ctx context.Context, vmid int, node string, values map[string]string) error {
	if len(values) == 0 {
		return fmt.Errorf("at least one --set key=value is required")
	}
	guest, err := s.resolve(ctx, vmid, node)
	if err != nil {
		return err
	}
	task, err := guest.Config(ctx, values)
	if err != nil {
		return err
	}
	return s.tasks.Handle(ctx, task)
}

func (s *GuestService) Delete(ctx context.Context, vmid int, node string) error {
	return s.run(ctx, vmid, node, func(guest Guest) (Task, error) {
		return guest.Delete(ctx)
	})
}

func (s *GuestService) Migrate(ctx context.Context, vmid int, node string, options MigrateOptions) error {
	if options.Target == "" {
		return fmt.Errorf("target node is required")
	}
	return s.run(ctx, vmid, node, func(guest Guest) (Task, error) {
		return guest.Migrate(ctx, options)
	})
}

func (s *GuestService) Resize(ctx context.Context, vmid int, node, disk, size string) error {
	if disk == "" {
		return fmt.Errorf("disk is required")
	}
	if size == "" {
		return fmt.Errorf("size is required")
	}
	return s.run(ctx, vmid, node, func(guest Guest) (Task, error) {
		return guest.Resize(ctx, disk, size)
	})
}

func (s *GuestService) ListSnapshots(ctx context.Context, vmid int, node string) ([]output.SnapshotRow, error) {
	guest, err := s.resolve(ctx, vmid, node)
	if err != nil {
		return nil, err
	}
	return guest.Snapshots(ctx)
}

func (s *GuestService) CreateSnapshot(ctx context.Context, vmid int, node, name string) error {
	name, err := normalizeSnapshotName(name)
	if err != nil {
		return err
	}
	return s.run(ctx, vmid, node, func(guest Guest) (Task, error) {
		return guest.CreateSnapshot(ctx, name)
	})
}

func (s *GuestService) RollbackSnapshot(ctx context.Context, vmid int, node, name string) error {
	name, err := normalizeSnapshotName(name)
	if err != nil {
		return err
	}
	return s.run(ctx, vmid, node, func(guest Guest) (Task, error) {
		return guest.RollbackSnapshot(ctx, name)
	})
}

func (s *GuestService) DeleteSnapshot(ctx context.Context, vmid int, node, name string) error {
	name, err := normalizeSnapshotName(name)
	if err != nil {
		return err
	}
	return s.run(ctx, vmid, node, func(guest Guest) (Task, error) {
		return guest.DeleteSnapshot(ctx, name)
	})
}

func (s *GuestService) run(ctx context.Context, vmid int, node string, action func(Guest) (Task, error)) error {
	guest, err := s.resolve(ctx, vmid, node)
	if err != nil {
		return err
	}
	task, err := action(guest)
	if err != nil {
		return err
	}
	return s.tasks.Handle(ctx, task)
}

func (s *GuestService) resolve(ctx context.Context, vmid int, node string) (Guest, error) {
	if vmid <= 0 {
		return nil, fmt.Errorf("invalid vmid %d", vmid)
	}
	if node != "" {
		return s.getOnNode(ctx, node, vmid)
	}

	nodes, err := s.backend.Nodes(ctx)
	if err != nil {
		return nil, err
	}

	var firstErr error
	for _, nodeRow := range nodes {
		if nodeRow.Name == "" {
			continue
		}
		guest, err := s.getOnNode(ctx, nodeRow.Name, vmid)
		if err == nil {
			return guest, nil
		}
		if firstErr == nil {
			firstErr = err
		}
		s.debug("skip node", "node", nodeRow.Name, "error", err)
	}

	if firstErr != nil {
		return nil, fmt.Errorf("%s %d not found", s.kind, vmid)
	}
	return nil, fmt.Errorf("%s %d not found", s.kind, vmid)
}

func (s *GuestService) listOnNode(ctx context.Context, node string) ([]output.GuestRow, error) {
	if s.kind == "vm" {
		return s.backend.VMs(ctx, node)
	}
	return s.backend.LXCs(ctx, node)
}

func (s *GuestService) getOnNode(ctx context.Context, node string, vmid int) (Guest, error) {
	if s.kind == "vm" {
		return s.backend.VM(ctx, node, vmid)
	}
	return s.backend.LXC(ctx, node, vmid)
}

func (s *GuestService) debug(msg string, args ...any) {
	if s.verbose && s.logger != nil {
		s.logger.Debug(msg, args...)
	}
}

func normalizeSnapshotName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("snapshot name is required")
	}
	return name, nil
}
