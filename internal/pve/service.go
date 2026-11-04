package pve

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lz-wang/pve-cli/v3/internal/output"
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
	runner := s.tasks
	runner.Resource = s.kind
	runner.VMID = int(result.NewVMID)
	runner.Node = result.TargetNode
	operation, err := runner.Execute(ctx, task)
	result.Resource = operation.Resource
	result.VMID = operation.VMID
	result.Node = operation.Node
	result.Status = operation.Status
	result.Error = operation.Error
	return result, err
}

// Config submits a native configuration write and verifies the outcome by
// re-reading the guest configuration whenever a reliable comparison is
// possible. The four-step contract is: submit, then re-read, then compare
// normalized values, then report confirmed/mismatched/unknown. Writes in an
// uncertain state are never retried.
func (s *GuestService) Config(ctx context.Context, vmid int, node string, values map[string]string) error {
	if len(values) == 0 {
		return fmt.Errorf("at least one --set key=value is required")
	}
	guest, err := s.resolve(ctx, vmid, node)
	if err != nil {
		return err
	}
	return s.submitAndVerify(ctx, guest, vmid, guest.Row().Node, values, values)
}

// submitAndVerify runs the shared write flow: submit the values, then verify
// the keys in verifyValues by re-reading the configuration. submitValues may
// carry extra request parameters (for example a concurrency digest) that are
// not part of the verified state.
func (s *GuestService) submitAndVerify(ctx context.Context, guest Guest, vmid int, guestNode string, submitValues, verifyValues map[string]string) error {
	runner := s.tasks
	runner.Resource = s.kind
	runner.VMID = vmid
	runner.Node = guestNode

	var submitErr error
	task, err := guest.Config(ctx, submitValues)
	if err != nil {
		for key, value := range submitValues {
			if sensitiveConfigKey(key) {
				err = redactError(err, value)
			}
		}
		submitErr = ClassifyError(err)
	}

	var operation output.OperationResult
	var priorErr error
	switch {
	case submitErr != nil:
		operation = output.OperationResult{Resource: s.kind, VMID: vmid, Node: guestNode, Status: "unknown"}
		priorErr = submitErr
	case task == nil:
		// Synchronous PVE write: the HTTP response carried no task UPID, so
		// the mutation is already applied and no async task exists to wait
		// for. Fabricating a task-timeout error here would corrupt the
		// result of an accepted write — state verification decides the
		// reported outcome (confirmed / mismatched / unverifiable).
		operation = output.OperationResult{Resource: s.kind, VMID: vmid, Node: guestNode, Status: "succeeded"}
	default:
		operation, priorErr = runner.Execute(ctx, task)
		// Async submissions and failed tasks keep their existing semantics;
		// verification only applies once the write settled (completed wait,
		// or a wait whose outcome is unknown).
		if operation.Status == "submitted" || (priorErr != nil && !errors.Is(priorErr, ErrTaskTimeout)) {
			return s.finishConfig(runner, operation, priorErr)
		}
	}

	outcome := s.verifyConfigWrite(ctx, guestNode, vmid, verifyValues)
	finalErr := applyVerification(&operation, priorErr, outcome)
	return s.finishConfig(runner, operation, finalErr)
}

// finishConfig writes the mutation result once and returns the command error.
func (s *GuestService) finishConfig(runner TaskRunner, operation output.OperationResult, err error) error {
	if runner.ResultWriter != nil {
		if e := output.WriteOperation(runner.ResultWriter, runner.Format, operation); e != nil {
			return e
		}
	}
	return err
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
	plan, err := s.PlanSnapshot(ctx, vmid, node, name, "rollback")
	if err != nil {
		return err
	}
	return s.run(ctx, vmid, plan.Node, func(guest Guest) (Task, error) {
		return guest.RollbackSnapshot(ctx, name)
	})
}

func (s *GuestService) DeleteSnapshot(ctx context.Context, vmid int, node, name string) error {
	name, err := normalizeSnapshotName(name)
	if err != nil {
		return err
	}
	plan, err := s.PlanSnapshot(ctx, vmid, node, name, "delete")
	if err != nil {
		return err
	}
	return s.run(ctx, vmid, plan.Node, func(guest Guest) (Task, error) {
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
	runner := s.tasks
	runner.Resource = s.kind
	runner.VMID = vmid
	runner.Node = guest.Row().Node
	return runner.Handle(ctx, task)
}

func (s *GuestService) resolve(ctx context.Context, vmid int, node string) (Guest, error) {
	if vmid <= 0 {
		return nil, fmt.Errorf("%w: invalid vmid %d", ErrInvalid, vmid)
	}
	if node != "" {
		g, err := s.getOnNode(ctx, node, vmid)
		return g, ClassifyError(err)
	}
	// Production backends use one cluster inventory request. Capability-only
	// backends use a complete traversal, preserving all non-NotFound errors.
	if _, ok := s.backend.(ClusterGuestBackend); ok {
		ref, err := s.Resolve(ctx, fmt.Sprint(vmid), node, false)
		if err != nil {
			return nil, err
		}
		g, err := s.getOnNode(ctx, ref.Node, vmid)
		return g, ClassifyError(err)
	}
	nodes, err := s.backend.Nodes(ctx)
	if err != nil {
		return nil, ClassifyError(err)
	}
	var found Guest
	for _, n := range nodes {
		g, e := s.getOnNode(ctx, n.Name, vmid)
		if e != nil {
			e = ClassifyError(e)
			if errors.Is(e, ErrNotFound) {
				continue
			}
			return nil, e
		}
		if found != nil {
			return nil, fmt.Errorf("%w: %s %d", ErrAmbiguous, s.kind, vmid)
		}
		found = g
	}
	if found == nil {
		return nil, fmt.Errorf("%w: %s %d not found", ErrNotFound, s.kind, vmid)
	}
	return found, nil
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
