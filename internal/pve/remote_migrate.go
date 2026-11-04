package pve

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

// RemoteMigrateOptions configures a cross-cluster migration through the
// source PVE's remote_migrate endpoint. The TargetEndpointBundle carries
// the target API token; it is passed to the API call only and must never
// reach logs, command output or error messages.
type RemoteMigrateOptions struct {
	TargetEndpointBundle string
	TargetStorageMap     map[string]string
	TargetBridgeMap      map[string]string
	TargetVMID           int
	BWLimit              uint
	Online               bool
}

// RemoteMigrator is the guest capability for cross-cluster migrations.
type RemoteMigrator interface {
	RemoteMigrate(ctx context.Context, options RemoteMigrateOptions) (Task, error)
}

// RemoteMigrate submits the cross-cluster migration on the source guest.
// Safety constraints: the migration is offline by default, the source
// container is never deleted, and the target is never started implicitly.
func (s *GuestService) RemoteMigrate(ctx context.Context, vmid int, node string, options RemoteMigrateOptions) (output.OperationResult, error) {
	if vmid <= 0 {
		return output.OperationResult{}, fmt.Errorf("%w: invalid vmid %d", ErrInvalid, vmid)
	}
	if strings.TrimSpace(options.TargetEndpointBundle) == "" {
		return output.OperationResult{}, fmt.Errorf("%w: a target endpoint bundle is required", ErrInvalid)
	}
	guest, err := s.resolve(ctx, vmid, node)
	if err != nil {
		return output.OperationResult{}, err
	}
	migrator, ok := guest.(RemoteMigrator)
	if !ok {
		return output.OperationResult{}, fmt.Errorf("%w: guest does not support remote migration", ErrInvalid)
	}

	// The remote_migrate API expects storage and bridge mapping parameters;
	// omitting them risks a server-side 400 before the migration starts.
	// With no explicit maps, derive explicit same-name mappings from the
	// guest configuration — the same semantics, stated instead of implied.
	if len(options.TargetStorageMap) == 0 || len(options.TargetBridgeMap) == 0 {
		derivedStorage, derivedBridge, deriveErr := s.deriveSameNameMaps(ctx, guest)
		if deriveErr != nil {
			return output.OperationResult{}, deriveErr
		}
		if len(options.TargetStorageMap) == 0 {
			options.TargetStorageMap = derivedStorage
		}
		if len(options.TargetBridgeMap) == 0 {
			options.TargetBridgeMap = derivedBridge
		}
	}

	task, err := migrator.RemoteMigrate(ctx, options)
	runner := s.tasks
	runner.Resource = s.kind
	runner.VMID = vmid
	runner.Node = guest.Row().Node
	if err != nil {
		// A failed submission must never surface as success — but only a
		// provable rejection marks the outcome failed. Server failures
		// (5xx) and transport errors leave it unknown: HTTP 500 does not
		// prove the migration did not start. A UPID returned alongside an
		// error is preserved so the task result can still be queried.
		status := TaskStatusUnknown
		if submissionRejected(err) {
			status = "failed"
		}
		operation := output.OperationResult{
			Resource: runner.Resource,
			VMID:     runner.VMID,
			Node:     runner.Node,
			Status:   status,
			Error:    err.Error(),
		}
		if task != nil {
			operation.Task = task.UPID()
		}
		if runner.ResultWriter != nil {
			if writeErr := output.WriteOperation(runner.ResultWriter, runner.Format, operation); writeErr != nil {
				return operation, writeErr
			}
		}
		return operation, fmt.Errorf("submit remote migration: %w", ClassifyError(err))
	}
	operation, runErr := runner.Execute(ctx, task)
	if runner.ResultWriter != nil {
		if writeErr := output.WriteOperation(runner.ResultWriter, runner.Format, operation); writeErr != nil {
			return operation, writeErr
		}
	}
	return operation, runErr
}

// deriveSameNameMaps reads the guest configuration and maps every referenced
// storage and bridge to itself: LXC rootfs/mp volumes and QEMU scsi/sata/
// virtio/ide disks decide the storage map; net devices decide the bridge
// map. Bind mounts (host paths) and devices without a bridge contribute
// nothing.
func (s *GuestService) deriveSameNameMaps(ctx context.Context, guest Guest) (map[string]string, map[string]string, error) {
	configBackend, ok := s.backend.(NativeConfigBackend)
	if !ok {
		return nil, nil, fmt.Errorf("%w: remote migration needs native config access to derive same-name storage and bridge mappings; pass them explicitly", ErrUnavailable)
	}
	row := guest.Row()
	config, err := configBackend.GuestConfig(ctx, s.kind, row.Node, int(row.VMID))
	if err != nil {
		return nil, nil, ClassifyError(err)
	}
	storages := map[string]string{}
	bridges := map[string]string{}
	for key, value := range config {
		text, ok := value.(string)
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "rootfs" || strings.HasPrefix(key, "mp") && s.kind == BackupKindLXC {
			if volume, _, found := strings.Cut(text, ":"); found {
				volume = strings.TrimSpace(volume)
				// Host paths (bind mounts) have no storage to map.
				if volume != "" && !strings.HasPrefix(volume, "/") {
					storages[volume] = volume
				}
			}
			continue
		}
		if s.kind == BackupKindVM && isQEMUDiskKey(key) {
			if volume, _, found := strings.Cut(text, ":"); found {
				volume = strings.TrimSpace(volume)
				if volume != "" && !strings.HasPrefix(volume, "/") {
					storages[volume] = volume
				}
			}
			continue
		}
		if lxcNetDevicePattern.MatchString(key) || isQEMUNetKey(key) {
			if bridge := optionAttributeValue(text, "bridge"); bridge != "" {
				bridges[bridge] = bridge
			}
		}
	}
	return storages, bridges, nil
}

// isQEMUDiskKey reports whether a QEMU configuration key is a migrated disk
// slot. efidisk and tpmstate are not storage-mapped disks.
func isQEMUDiskKey(key string) bool {
	for _, prefix := range []string{"scsi", "sata", "virtio", "ide"} {
		if rest, found := strings.CutPrefix(key, prefix); found {
			return rest != "" && isAllDigits(rest)
		}
	}
	return false
}

// isQEMUNetKey reports whether a QEMU configuration key is a network device.
func isQEMUNetKey(key string) bool {
	return strings.HasPrefix(key, "net") && isAllDigits(strings.TrimPrefix(key, "net"))
}

func isAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// submissionRejected reports whether an error proves PVE rejected the
// migration before it started: a structured APIError in the 4xx family
// (parameter validation, permission, conflict, optimistic-concurrency
// digest), or a server error whose body names a missing resource — a lookup
// failure cannot have started anything. Everything else (5xx, connection
// interruptions, timeouts) leaves the outcome unproven: the caller must
// re-check the actual task state instead of trusting a mechanical HTTP
// classification.
func submissionRejected(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
		return true
	}
	return apiErr.HTTPStatus >= 500 && errors.Is(apiErr, ErrNotFound)
}

// formatMigrationMaps renders a source=target map in PVE's official
// "source:target" pair form ("a:b,c:d").
func formatMigrationMaps(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	pairs := make([]string, 0, len(names))
	for _, name := range names {
		pairs = append(pairs, name+":"+m[name])
	}
	return strings.Join(pairs, ",")
}

// lxcGuest remote migration over the go-proxmox wrapper.
func (g lxcGuest) RemoteMigrate(ctx context.Context, options RemoteMigrateOptions) (Task, error) {
	params := &proxmox.ContainerRemoteMigrateOptions{
		TargetEndpoint: options.TargetEndpointBundle,
		TargetStorage:  formatMigrationMaps(options.TargetStorageMap),
		TargetBridge:   formatMigrationMaps(options.TargetBridgeMap),
		TargetVMID:     options.TargetVMID,
	}
	if options.BWLimit > 0 {
		params.BWLimit = uint64(options.BWLimit)
	}
	if options.Online {
		params.Online = proxmox.IntOrBool(true)
	}
	// Delete and Restart are intentionally never set: the source container
	// is preserved and the target is not restarted implicitly.
	task, err := g.ct.RemoteMigrate(ctx, params)
	return wrapTask(task), err
}
