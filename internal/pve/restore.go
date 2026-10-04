package pve

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pvectl/internal/output"
)

// RestoreOptions describes a one-off restore of a vzdump backup archive into
// a new, non-existing VMID.
type RestoreOptions struct {
	Kind    string
	Archive string
	Node    string
	VMID    int
	Storage string
}

// RestoreBackend covers one-off backup restore plus the guest and node
// lookups needed for validation.
type RestoreBackend interface {
	GuestBackend
	Restore(ctx context.Context, options RestoreOptions) (Task, error)
}

// RestoreService validates and executes one-off backup restores.
type RestoreService struct {
	backend RestoreBackend
	tasks   TaskRunner
	logger  *slog.Logger
	verbose bool
}

func NewRestoreService(backend RestoreBackend, tasks TaskRunner, logger *slog.Logger, verbose bool) *RestoreService {
	return &RestoreService{backend: backend, tasks: tasks, logger: logger, verbose: verbose}
}

// Restore validates the request, refuses existing VMIDs, and triggers the
// restore task. Restore never overwrites an existing guest; delete the guest
// first instead.
func (s *RestoreService) Restore(ctx context.Context, options RestoreOptions) (output.RestoreResult, error) {
	kind, err := parseRestoreKind(options.Kind)
	if err != nil {
		return output.RestoreResult{}, err
	}
	archive := strings.TrimSpace(options.Archive)
	if archive == "" {
		return output.RestoreResult{}, fmt.Errorf("archive is required")
	}
	node := strings.TrimSpace(options.Node)
	if node == "" {
		return output.RestoreResult{}, fmt.Errorf("node is required")
	}
	if options.VMID <= 0 {
		return output.RestoreResult{}, fmt.Errorf("invalid vmid %d", options.VMID)
	}
	if err := checkRestoreArchiveKind(kind, archive); err != nil {
		return output.RestoreResult{}, err
	}
	if err := s.ensureVMIDFree(ctx, options.VMID); err != nil {
		return output.RestoreResult{}, err
	}

	task, err := s.backend.Restore(ctx, RestoreOptions{
		Kind:    kind,
		Archive: archive,
		Node:    node,
		VMID:    options.VMID,
		Storage: strings.TrimSpace(options.Storage),
	})
	if err != nil {
		return output.RestoreResult{}, err
	}

	result := output.RestoreResult{
		Kind:    kind,
		VMID:    uint64(options.VMID),
		Node:    node,
		Archive: archive,
		Storage: strings.TrimSpace(options.Storage),
	}
	if task != nil {
		result.Task = task.UPID()
	}
	// The restore task is already submitted when the wait fails, so the
	// structured result must survive for stdout; `task wait` follows the
	// same pattern.
	if err := s.tasks.Handle(ctx, task); err != nil {
		return result, err
	}
	return result, nil
}

func parseRestoreKind(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case BackupKindVM:
		return BackupKindVM, nil
	case BackupKindLXC:
		return BackupKindLXC, nil
	default:
		return "", fmt.Errorf("invalid restore kind %q, expected vm or lxc", value)
	}
}

// checkRestoreArchiveKind verifies the backup kind encoded in common vzdump
// archive names when possible; unrecognized archive shapes are allowed.
func checkRestoreArchiveKind(kind, archive string) error {
	archiveKind := inferBackupKind(path.Base(archive))
	if archiveKind != BackupKindVM && archiveKind != BackupKindLXC {
		return nil
	}
	if archiveKind != kind {
		return fmt.Errorf("archive %s contains a %s backup, not a %s backup", archive, archiveKind, kind)
	}
	return nil
}

// ensureVMIDFree refuses restores into VMIDs that already exist anywhere in
// the cluster. The check must be reliable: a mutation has to fail closed, so
// when any node's guest inventory cannot be queried the restore aborts
// before anything is started.
func (s *RestoreService) ensureVMIDFree(ctx context.Context, vmid int) error {
	nodes, err := s.backend.Nodes(ctx)
	if err != nil {
		return fmt.Errorf("verify vmid %d: %w", vmid, err)
	}

	for _, node := range nodes {
		if node.Name == "" {
			continue
		}
		if err := s.checkVMIDFreeOnNode(ctx, node.Name, vmid); err != nil {
			return err
		}
	}
	return nil
}

func (s *RestoreService) checkVMIDFreeOnNode(ctx context.Context, nodeName string, vmid int) error {
	vmRows, err := s.backend.VMs(ctx, nodeName)
	if err != nil {
		return fmt.Errorf("verify vmid %d: list vm on node %s: %w", vmid, nodeName, err)
	}
	for _, row := range vmRows {
		if row.VMID == uint64(vmid) {
			return fmt.Errorf("vmid %d already exists on node %s; delete it first or choose another vmid", vmid, nodeName)
		}
	}

	lxcRows, err := s.backend.LXCs(ctx, nodeName)
	if err != nil {
		return fmt.Errorf("verify vmid %d: list lxc on node %s: %w", vmid, nodeName, err)
	}
	for _, row := range lxcRows {
		if row.VMID == uint64(vmid) {
			return fmt.Errorf("vmid %d already exists on node %s; delete it first or choose another vmid", vmid, nodeName)
		}
	}
	return nil
}

// Restore triggers the restore task on the PVE side. Both kinds reuse the
// guest create endpoints through typed wrappers: VM restore passes archive
// to the qemu create endpoint; LXC restore passes ostemplate plus
// restore=true to the lxc create endpoint, matching `pct restore`.
func (b *ProxmoxBackend) Restore(ctx context.Context, options RestoreOptions) (Task, error) {
	nodeName := strings.TrimSpace(options.Node)
	if nodeName == "" {
		return nil, fmt.Errorf("node is required")
	}
	if options.VMID <= 0 {
		return nil, fmt.Errorf("invalid vmid %d", options.VMID)
	}
	archive := strings.TrimSpace(options.Archive)
	if archive == "" {
		return nil, fmt.Errorf("archive is required")
	}

	node, err := b.client.Node(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	storage := strings.TrimSpace(options.Storage)

	if options.Kind == BackupKindLXC {
		// The lxc create endpoint rejects archive; a backup archive is
		// restored via ostemplate plus restore=true, like `pct restore`.
		opts := []proxmox.ContainerOption{
			{Name: "ostemplate", Value: archive},
			{Name: "restore", Value: true},
		}
		if storage != "" {
			opts = append(opts, proxmox.ContainerOption{Name: "storage", Value: storage})
		}
		task, err := node.NewContainer(ctx, options.VMID, opts...)
		return wrapTask(task), err
	}

	opts := []proxmox.VirtualMachineOption{
		{Name: "archive", Value: archive},
	}
	if storage != "" {
		opts = append(opts, proxmox.VirtualMachineOption{Name: "storage", Value: storage})
	}
	task, err := node.NewVirtualMachine(ctx, options.VMID, opts...)
	return wrapTask(task), err
}
