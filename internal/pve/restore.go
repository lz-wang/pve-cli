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
	if err := s.tasks.Handle(ctx, task); err != nil {
		return output.RestoreResult{}, err
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
// the cluster. Node-level listing failures are tolerated because the restore
// itself will still fail server-side if the VMID is taken.
func (s *RestoreService) ensureVMIDFree(ctx context.Context, vmid int) error {
	nodes, err := s.backend.Nodes(ctx)
	if err != nil {
		return fmt.Errorf("verify vmid %d: %w", vmid, err)
	}

	for _, node := range nodes {
		if node.Name == "" {
			continue
		}
		if owner, ok := s.findGuestOwner(ctx, node.Name, vmid); ok {
			return fmt.Errorf("vmid %d already exists on node %s; delete it first or choose another vmid", vmid, owner)
		}
	}
	return nil
}

func (s *RestoreService) findGuestOwner(ctx context.Context, nodeName string, vmid int) (string, bool) {
	vmRows, err := s.backend.VMs(ctx, nodeName)
	if err == nil {
		for _, row := range vmRows {
			if row.VMID == uint64(vmid) {
				return nodeName, true
			}
		}
	} else {
		s.debug("skip vm list", "node", nodeName, "error", err)
	}

	lxcRows, err := s.backend.LXCs(ctx, nodeName)
	if err == nil {
		for _, row := range lxcRows {
			if row.VMID == uint64(vmid) {
				return nodeName, true
			}
		}
	} else {
		s.debug("skip lxc list", "node", nodeName, "error", err)
	}
	return "", false
}

func (s *RestoreService) debug(msg string, args ...any) {
	if s.verbose && s.logger != nil {
		s.logger.Debug(msg, args...)
	}
}

// Restore triggers the restore task on the PVE side. VM restore reuses the
// qemu create endpoint with an archive parameter; LXC restore posts to the
// lxc create endpoint, which has no typed wrapper upstream yet.
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
		data := map[string]interface{}{
			"vmid":    options.VMID,
			"archive": archive,
		}
		if storage != "" {
			data["storage"] = storage
		}
		var upid proxmox.UPID
		if err := b.client.Post(ctx, fmt.Sprintf("/nodes/%s/lxc", nodeName), data, &upid); err != nil {
			return nil, err
		}
		return wrapTask(proxmox.NewTask(upid, b.client)), nil
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
