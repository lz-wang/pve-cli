package pve

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"
	"github.com/lz-wang/pve-cli/v3/internal/output"
)

type BackupDeleteBackend interface {
	BackupReader
	DeleteBackup(context.Context, string, string) (Task, error)
}

// DeleteBackup uses the SDK client. PVE may return null for synchronous deletion.
func (b *ProxmoxBackend) DeleteBackup(ctx context.Context, node, archive string) (Task, error) {
	storage, _, err := validateDeletableArchive(archive)
	if err != nil {
		return nil, err
	}
	var upid proxmox.UPID
	err = b.client.Delete(ctx, "/nodes/"+url.PathEscape(node)+"/storage/"+url.PathEscape(storage)+"/content/"+url.PathEscape(archive), &upid)
	var task Task
	if strings.TrimSpace(string(upid)) != "" {
		task = wrapTask(proxmox.NewTask(upid, b.client))
	}
	if err != nil {
		return task, ClassifyError(err)
	}
	return task, nil
}

var deletableArchive = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*:backup/vzdump-(?:lxc|qemu)-[1-9][0-9]*-[A-Za-z0-9_-]+\.(?:tar|vma)(?:\.(?:zst|gz|lzo))?$`)

func validateDeletableArchive(archive string) (string, string, error) {
	if !deletableArchive.MatchString(archive) {
		return "", "", fmt.Errorf("%w: select one exact vzdump archive volume ID; paths, patterns and PBS snapshots are not supported", ErrInvalid)
	}
	return parseBackupVolid(archive)
}

// InspectBackupDelete returns the exact unprotected archive. It never infers ownership from a VMID.
func InspectBackupDelete(ctx context.Context, b BackupReader, node, archive string) (output.BackupRow, error) {
	storage, _, err := validateDeletableArchive(archive)
	if err != nil {
		return output.BackupRow{}, err
	}
	if strings.TrimSpace(node) == "" {
		return output.BackupRow{}, fmt.Errorf("%w: node is required", ErrInvalid)
	}
	rows, err := b.Backups(ctx, node, storage)
	if err != nil {
		return output.BackupRow{}, err
	}
	var match *output.BackupRow
	for _, r := range rows {
		if r.VolID == archive {
			if match != nil {
				return output.BackupRow{}, fmt.Errorf("%w: ambiguous archive inventory", ErrConflict)
			}
			copy := r
			match = &copy
		}
	}
	if match == nil {
		return output.BackupRow{}, fmt.Errorf("%w: archive not found", ErrNotFound)
	}
	if match.Protected != "" && match.Protected != "0" {
		return *match, fmt.Errorf("%w: archive is protected", ErrConflict)
	}
	return *match, nil
}

// DeleteBackupArchive rechecks the confirmed archive before the single deletion request.
// A successful result requires a subsequent inventory to confirm absence.
func DeleteBackupArchive(ctx context.Context, b BackupDeleteBackend, tasks TaskRunner, node string, expected output.BackupRow) (output.BackupDeleteResult, error) {
	result := output.BackupDeleteResult{OperationResult: output.OperationResult{Resource: "backup", Node: node, Status: "failed"}, Archive: expected.VolID}
	current, err := InspectBackupDelete(ctx, b, node, expected.VolID)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	if current.Size != expected.Size || current.CTime != expected.CTime || current.Kind != expected.Kind || current.VMID != expected.VMID {
		result.Error = "archive changed after confirmation"
		return result, fmt.Errorf("%w: %s", ErrConflict, result.Error)
	}
	task, err := b.DeleteBackup(ctx, node, expected.VolID)
	if task != nil {
		result.Task = task.UPID()
	}
	if err != nil {
		result.Status = "unknown"
		if submissionRejected(err) {
			result.Status = "failed"
		}
		result.Error = err.Error()
		if result.Status == "unknown" {
			err = fmt.Errorf("%w: %w", ErrUncertain, err)
		}
		return result, err
	}
	if task != nil {
		tasks.Resource = "backup"
		tasks.Node = node
		result.OperationResult, err = tasks.Execute(ctx, task)
		if err != nil || result.Status != "succeeded" {
			return result, err
		}
	}
	storage, _, _ := parseBackupVolid(expected.VolID)
	rows, err := b.Backups(ctx, node, storage)
	if err != nil {
		result.Status = "unknown"
		result.Error = "deletion submitted but archive inventory could not be read"
		return result, fmt.Errorf("%w: %s", ErrUncertain, result.Error)
	}
	for _, row := range rows {
		if row.VolID == expected.VolID {
			result.Status = "unknown"
			result.Error = "deletion submitted but archive remains listed"
			verified := false
			result.Verified = &verified
			return result, fmt.Errorf("%w: %s", ErrUncertain, result.Error)
		}
	}
	result.Status = "succeeded"
	verified := true
	result.Verified = &verified
	return result, nil
}
