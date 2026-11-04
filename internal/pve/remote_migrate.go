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

	task, err := migrator.RemoteMigrate(ctx, options)
	runner := s.tasks
	runner.Resource = s.kind
	runner.VMID = vmid
	runner.Node = guest.Row().Node
	if err != nil {
		// A failed submission must never surface as success. An APIError
		// proves PVE rejected the request (nothing was committed); any
		// other failure leaves the submission state unconfirmed.
		status := TaskStatusUnknown
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			status = "failed"
		}
		operation := output.OperationResult{
			Resource: runner.Resource,
			VMID:     runner.VMID,
			Node:     runner.Node,
			Status:   status,
			Error:    err.Error(),
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

// formatMigrationMaps renders a source=target map in PVE's "a=b,c=d" form.
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
		pairs = append(pairs, name+"="+m[name])
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
