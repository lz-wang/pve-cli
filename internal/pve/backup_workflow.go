package pve

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

// BackupCopyRestoreOptions drives the composed cross-profile workflow:
// backup → transfer → verify (inside the transfer) → restore → post-check.
type BackupCopyRestoreOptions struct {
	VMID          int
	BackupStorage string // source dir storage for the backup stage
	// Archive starts the workflow from an existing archive (backup stage
	// skipped); used to resume interrupted runs.
	Archive       string
	TargetStorage string // dir storage on the target host
	TargetNode    string
	TargetVMID    int
	BridgeMap     map[string]string
}

// BackupCopyRestoreWorkflow composes the existing services. Every stage
// records its own result and error state; a failing stage stops the
// workflow without any automatic rollback — both sides stay as they are
// for manual repair and a documented resume.
type BackupCopyRestoreWorkflow struct {
	backups   *BackupService
	transfers *BackupTransferService
	restores  *RestoreService
	guests    *GuestService
	target    GuestBackend
}

func NewBackupCopyRestoreWorkflow(backups *BackupService, transfers *BackupTransferService, restores *RestoreService, guests *GuestService, target GuestBackend) *BackupCopyRestoreWorkflow {
	return &BackupCopyRestoreWorkflow{backups: backups, transfers: transfers, restores: restores, guests: guests, target: target}
}

// Run executes the pipeline. An existing target guest refuses the workflow
// outright: the same VMID is not proof that this workflow created it, and
// there is no persisted run identity to prove a resume. An explicit Archive
// skips the backup stage for documented, operator-verified continuations.
func (w *BackupCopyRestoreWorkflow) Run(ctx context.Context, options BackupCopyRestoreOptions) (output.WorkflowResult, error) {
	if options.VMID <= 0 {
		return output.WorkflowResult{}, fmt.Errorf("%w: invalid vmid %d", ErrInvalid, options.VMID)
	}
	if strings.TrimSpace(options.BackupStorage) == "" && strings.TrimSpace(options.Archive) == "" {
		return output.WorkflowResult{}, fmt.Errorf("%w: --backup-storage or --archive is required", ErrInvalid)
	}
	if strings.TrimSpace(options.TargetStorage) == "" {
		return output.WorkflowResult{}, fmt.Errorf("%w: --target-storage is required", ErrInvalid)
	}
	if strings.TrimSpace(options.TargetNode) == "" {
		return output.WorkflowResult{}, fmt.Errorf("%w: --target-node is required", ErrInvalid)
	}
	if options.TargetVMID == 0 {
		options.TargetVMID = options.VMID
	}

	result := output.WorkflowResult{
		Resource:   "lxc",
		VMID:       options.VMID,
		TargetVMID: options.TargetVMID,
		TargetNode: options.TargetNode,
	}
	fail := func(stageErr error) (output.WorkflowResult, error) {
		result.Status = "failed"
		return result, stageErr
	}

	targetExists, err := w.targetGuestExists(ctx, options)
	if err != nil {
		detail := fmt.Sprintf("target guest inventory for vmid %d unreadable (%v); refusing to proceed", options.TargetVMID, ClassifyError(err))
		result.Stages = append(result.Stages, output.WorkflowStageResult{Stage: "preflight", Status: "failed", Detail: detail})
		return fail(fmt.Errorf("%w: %s", ErrUnavailable, detail))
	}
	if targetExists {
		detail := fmt.Sprintf("target vmid %d already exists on target node %s; it is not known to originate from this workflow — remove it manually or choose another --target-vmid; to reuse a verified archive after cleanup, re-run with --archive", options.TargetVMID, options.TargetNode)
		result.Stages = append(result.Stages, output.WorkflowStageResult{Stage: "preflight", Status: "failed", Detail: detail})
		return fail(fmt.Errorf("%w: %s", ErrConflict, detail))
	}
	archive := strings.TrimSpace(options.Archive)
	if archive != "" {
		result.Archive = archive
		result.Stages = append(result.Stages, skippedStage("backup", "explicit archive given"))
	} else {
		volid, stage := w.backupStage(ctx, options)
		result.Stages = append(result.Stages, stage)
		if stage.Status != "succeeded" {
			return fail(fmt.Errorf("backup stage: %s", stage.Detail))
		}
		archive = volid
		result.Archive = archive
	}

	transferResult, err := w.transfers.Copy(ctx, BackupCopyOptions{Volid: archive, TargetStorage: options.TargetStorage})
	stage := output.WorkflowStageResult{Stage: "transfer", Status: "succeeded", Detail: fmt.Sprintf("%s -> %s (%s, %s)", transferResult.SourceVolid, transferResult.TargetPath, output.FormatBytes(transferResult.Bytes), transferResult.SHA256[:min(12, len(transferResult.SHA256))])}
	if err != nil {
		stage.Status = "failed"
		stage.Detail = err.Error()
		result.Stages = append(result.Stages, stage)
		return fail(fmt.Errorf("transfer stage: %w", err))
	}
	result.Stages = append(result.Stages, stage)

	restoreResult, err := w.restores.Restore(ctx, RestoreOptions{
		Kind:      BackupKindLXC,
		Archive:   fmt.Sprintf("%s:%s", options.TargetStorage, strings.SplitN(archive, ":", 2)[1]),
		Node:      options.TargetNode,
		VMID:      options.TargetVMID,
		BridgeMap: options.BridgeMap,
	})
	stage = output.WorkflowStageResult{Stage: "restore", Status: restoreResult.Status, Task: restoreResult.Task, Detail: restoreResult.Archive}
	if err != nil || restoreResult.Status != "succeeded" {
		if err != nil && restoreResult.Status == "succeeded" {
			// The restore task completed but the post-restore configuration
			// failed: report the stage as partial, name the repair
			// responsibility, and fail the workflow.
			stage.Status = "partial"
			stage.Detail = fmt.Sprintf("%s; post-restore configuration failed: %v — repair the mapping manually (or re-run after fixing) before using the guest", restoreResult.Archive, err)
		} else {
			if stage.Status == "" {
				stage.Status = "failed"
			}
			stage.Detail = detailOrError(restoreResult.Error, err)
		}
		result.Stages = append(result.Stages, stage)
		if err != nil {
			return fail(fmt.Errorf("restore stage: %w", err))
		}
		return fail(fmt.Errorf("restore stage: %s", restoreResult.Error))
	}
	result.Stages = append(result.Stages, stage)

	plan, err := w.guests.CheckRemoteMigration(ctx, options.TargetVMID, RemoteMigrationCheckOptions{
		SourceVMID: options.VMID,
		Target:     w.target,
		TargetNode: options.TargetNode,
	})
	if err != nil {
		stage := failedStage("post-check", err.Error())
		result.Stages = append(result.Stages, stage)
		return fail(fmt.Errorf("post-check stage: %w", err))
	}
	detail := fmt.Sprintf("plan status %s (%d checks)", plan.Status, len(plan.Checks))
	status := "succeeded"
	switch plan.Status {
	case "failed":
		status = "failed"
		detail = detail + "; failing checks: " + failingChecks(plan)
	case "warning":
		// Unconfirmed risks (MAC/IP, DHCP, autostart) are not observable via
		// the API: the target must not be reported unconditionally safe.
		status = "warning"
		detail = detail + "; unconfirmed checks: " + warningChecks(plan)
	}
	result.Stages = append(result.Stages, output.WorkflowStageResult{Stage: "post-check", Status: status, Detail: detail})
	if status == "failed" {
		result.Status = "failed"
		return result, fmt.Errorf("post-check stage: %s", failingChecks(plan))
	}
	result.Status = status
	return result, nil
}

// warningChecks lists the unknown/warning check names for the warning detail.
func warningChecks(plan output.MigrationPlan) string {
	var names []string
	for _, row := range plan.Checks {
		if row.Status == output.MigrationCheckWarning || row.Status == output.MigrationCheckUnknown {
			names = append(names, row.Check)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// backupStage creates the backup and resolves the archive this run created.
// The listing is correlated against the task start time: the newest listing
// entry proves nothing about its origin, so an archive created before the
// task is never accepted.
func (w *BackupCopyRestoreWorkflow) backupStage(ctx context.Context, options BackupCopyRestoreOptions) (string, output.WorkflowStageResult) {
	// Clock-skew tolerance between the CLI host and the PVE host: archives
	// stamped slightly before the observed start still count as this run's.
	const clockSkewTolerance = 2 * time.Minute
	started := time.Now().Add(-clockSkewTolerance)

	backup, err := w.backups.BackupGuest(ctx, BackupCreateOptions{
		Kind:    BackupKindLXC,
		VMID:    options.VMID,
		Storage: options.BackupStorage,
	})
	if err != nil || backup.Status != "succeeded" {
		status := backup.Status
		if status == "" {
			status = "failed"
		}
		return "", output.WorkflowStageResult{Stage: "backup", Status: status, Task: backup.Task, Detail: detailOrError(backup.Error, err)}
	}
	reader, ok := w.backups.backend.(BackupReader)
	if !ok {
		return "", failedStage("backup", "backup listing unavailable")
	}
	sourceNode := backup.Node
	rows, err := reader.Backups(ctx, sourceNode, options.BackupStorage)
	if err != nil {
		return "", failedStage("backup", err.Error())
	}
	var latest output.BackupRow
	var newestAny output.BackupRow
	found := false
	sawAny := false
	for _, row := range rows {
		if row.VMID != uint64(options.VMID) || row.Kind != BackupKindLXC {
			continue
		}
		if !sawAny || row.CTime > newestAny.CTime {
			newestAny = row
			sawAny = true
		}
		if row.CTime >= uint64(started.Unix()) && (!found || row.CTime > latest.CTime) {
			latest = row
			found = true
		}
	}
	if !found {
		detail := "backup completed but no archive created by this run was listed"
		if newestAny.VolID != "" {
			detail += fmt.Sprintf("; newest older candidate is %s (created %s)", newestAny.VolID, time.Unix(int64(newestAny.CTime), 0).UTC().Format(time.RFC3339))
		}
		detail += "; re-run with --archive <volid> to continue from a verified archive"
		return "", failedStage("backup", detail)
	}
	return latest.VolID, output.WorkflowStageResult{Stage: "backup", Status: "succeeded", Task: backup.Task, Detail: latest.VolID}
}

func (w *BackupCopyRestoreWorkflow) targetGuestExists(ctx context.Context, options BackupCopyRestoreOptions) (bool, error) {
	nodes, err := w.target.Nodes(ctx)
	if err != nil {
		return false, err
	}
	for _, node := range nodes {
		if options.TargetNode != "" && node.Name != options.TargetNode {
			continue
		}
		guest, err := w.target.LXC(ctx, node.Name, options.TargetVMID)
		if err == nil && guest != nil {
			return true, nil
		}
		if !errors.Is(ClassifyError(err), ErrNotFound) {
			return false, err
		}
	}
	return false, nil
}

func skippedStage(name, detail string) output.WorkflowStageResult {
	return output.WorkflowStageResult{Stage: name, Status: "skipped", Detail: detail}
}

func failedStage(name, detail string) output.WorkflowStageResult {
	return output.WorkflowStageResult{Stage: name, Status: "failed", Detail: detail}
}

func detailOrError(detail string, err error) string {
	if err != nil {
		return err.Error()
	}
	return detail
}

func failingChecks(plan output.MigrationPlan) string {
	var reasons []string
	for _, row := range plan.Checks {
		if row.Status == output.MigrationCheckFail {
			reasons = append(reasons, row.Check+": "+row.Message)
		}
	}
	sort.Strings(reasons)
	if len(reasons) == 0 {
		return "unknown"
	}
	return strings.Join(reasons, "; ")
}
