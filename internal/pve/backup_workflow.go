package pve

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
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
	Archive        string
	TargetStorage  string // dir storage on the target host
	RestoreStorage string // guest disk storage on the target host; empty uses the PVE default
	TargetNode     string
	TargetVMID     int
	BridgeMap      map[string]string
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
	// The overall status carries the stopping stage's own status: a stage
	// that cannot prove its outcome keeps the workflow unknown (rerunning
	// blindly could duplicate the operation), a stage that completed its
	// main effect while a post-step failed keeps partial, and only a
	// definite rejection or failure reports failed.
	fail := func(stageStatus string, stageErr error) (output.WorkflowResult, error) {
		result.Status = aggregateWorkflowStatus(stageStatus)
		// P1-4: an unconfirmed workflow outcome must exit 5 (unknown final
		// outcome — cannot safely retry), never the generic 1. ErrUncertain
		// rides every unknown-stage error chain — including wait timeouts
		// that already carry ErrTaskTimeout — so automation checks one
		// sentinel. Definite failures keep their classified exit codes.
		if result.Status == "unknown" && stageErr != nil && !errors.Is(stageErr, ErrUncertain) {
			stageErr = fmt.Errorf("%w: %w", ErrUncertain, stageErr)
		}
		return result, stageErr
	}

	// The VMID namespace is cluster-global: a VM or container on ANY node
	// occupies the target VMID. The workflow reuses the restore service's
	// global check, so the preflight and the restore preflight can never
	// disagree about what counts as occupied.
	if vmidErr := w.restores.VMIDFree(ctx, options.TargetVMID); vmidErr != nil {
		if errors.Is(vmidErr, ErrConflict) {
			detail := fmt.Sprintf("target vmid %d already exists in the target cluster; it is not known to originate from this workflow — remove it manually or choose another --target-vmid; to reuse a verified archive after cleanup, re-run with --archive", options.TargetVMID)
			result.Stages = append(result.Stages, output.WorkflowStageResult{Stage: "preflight", Status: "failed", Detail: detail})
			return fail("failed", fmt.Errorf("%w: %s", ErrConflict, detail))
		}
		detail := fmt.Sprintf("target guest inventory for vmid %d unreadable (%v); refusing to proceed", options.TargetVMID, vmidErr)
		result.Stages = append(result.Stages, output.WorkflowStageResult{Stage: "preflight", Status: "failed", Detail: detail})
		return fail("failed", fmt.Errorf("%w: %s", ErrUnavailable, detail))
	}
	archive := strings.TrimSpace(options.Archive)
	if archive != "" {
		result.Archive = archive
		result.Stages = append(result.Stages, skippedStage("backup", "explicit archive given"))
	} else {
		volid, stage := w.backupStage(ctx, options)
		result.Stages = append(result.Stages, stage)
		if stage.Status != "succeeded" {
			return fail(stage.Status, fmt.Errorf("backup stage: %s", stage.Detail))
		}
		archive = volid
		result.Archive = archive
	}

	transferResult, err := w.transfers.Copy(ctx, BackupCopyOptions{Volid: archive, TargetStorage: options.TargetStorage})
	stage := output.WorkflowStageResult{Stage: "transfer", Status: "succeeded", Detail: fmt.Sprintf("%s -> %s (%s, %s)", transferResult.SourceVolid, transferResult.TargetPath, output.FormatBytes(transferResult.Bytes), transferResult.SHA256[:min(12, len(transferResult.SHA256))])}
	if err != nil {
		stage.Status = transferResult.Status
		if stage.Status == "" {
			stage.Status = "failed"
		}
		stage.Detail = err.Error()
		result.Stages = append(result.Stages, stage)
		return fail(stage.Status, fmt.Errorf("transfer stage: %w", err))
	}
	result.Stages = append(result.Stages, stage)

	restoreResult, err := w.restores.Restore(ctx, RestoreOptions{
		Kind:      BackupKindLXC,
		Archive:   fmt.Sprintf("%s:%s", options.TargetStorage, strings.SplitN(archive, ":", 2)[1]),
		Node:      options.TargetNode,
		VMID:      options.TargetVMID,
		BridgeMap: options.BridgeMap,
		Storage:   options.RestoreStorage,
	})
	// The restore service reports the split state itself: status is partial
	// when the archive restored but the post-restore configuration failed.
	stage = output.WorkflowStageResult{Stage: "restore", Status: restoreResult.Status, Task: restoreResult.Task, Detail: restoreResult.Archive}
	if err != nil || restoreResult.Status != "succeeded" {
		if restoreResult.Status == "partial" {
			stage.Detail = fmt.Sprintf("%s; post-restore configuration failed: %s — repair the mapping manually (or re-run after fixing) before using the guest", restoreResult.Archive, restoreResult.Error)
		} else {
			if stage.Status == "" {
				stage.Status = "failed"
			}
			stage.Detail = detailOrError(restoreResult.Error, err)
		}
		result.Stages = append(result.Stages, stage)
		if err != nil {
			return fail(stage.Status, fmt.Errorf("restore stage: %w", err))
		}
		return fail(stage.Status, fmt.Errorf("restore stage: %s", restoreResult.Error))
	}
	result.Stages = append(result.Stages, stage)

	plan, err := w.guests.CheckRemoteMigration(ctx, options.TargetVMID, RemoteMigrationCheckOptions{
		SourceVMID: options.VMID,
		Target:     w.target,
		TargetNode: options.TargetNode,
	})
	if err != nil {
		// The read-only check could not run; everything before it succeeded.
		// The unconfirmed safety state is a warning, never a failed that
		// hides the restored guest.
		stage := output.WorkflowStageResult{Stage: "post-check", Status: "warning", Detail: err.Error()}
		result.Stages = append(result.Stages, stage)
		return fail("warning", fmt.Errorf("post-check stage: %w", err))
	}
	detail := fmt.Sprintf("plan status %s (%d checks)", plan.Status, len(plan.Checks))
	status := "succeeded"
	switch plan.Status {
	case "failed":
		// The check ran and failed, but every mutating stage succeeded and
		// the restored guest exists: the workflow is partial (its main
		// effect completed, its safety verification failed) — the stage
		// keeps the definite check result and the error names the reasons.
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
		return fail("partial", fmt.Errorf("post-check stage: %s", failingChecks(plan)))
	}
	result.Status = status
	return result, nil
}

// aggregateWorkflowStatus maps the stopping stage's status onto the overall
// workflow status. definite failures stay failed; a stage that cannot prove
// its outcome (unknown, or an unwaited submitted task) keeps the workflow
// unknown — the caller must re-check the submitted tasks instead of
// rerunning blindly; a stage that completed its main effect while a
// post-step failed (partial) keeps the completed fact visible; a completed
// pipeline whose safety verification could not run reports warning.
func aggregateWorkflowStatus(stageStatus string) string {
	switch stageStatus {
	case "partial":
		return "partial"
	case "unknown", "submitted":
		return "unknown"
	case "warning":
		return "warning"
	default:
		return "failed"
	}
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
// The archive is identified from the vzdump task log when the log names one
// — a concurrently started backup of the same guest can then never be
// misattributed. The task-start time window stays as the fallback for logs
// that are unreadable or name no archive.
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
	// P2: prefer the archive the finished task's log names; the log is the
	// only record that proves which file vzdump actually wrote.
	loggedArchive := archiveNameFromTaskLog(ctx, w.backups.backend, backup.Task)
	var latest output.BackupRow
	var newestAny output.BackupRow
	var logMatch *output.BackupRow
	found := false
	sawAny := false
	for i, row := range rows {
		if row.VMID != uint64(options.VMID) || row.Kind != BackupKindLXC {
			continue
		}
		if !sawAny || row.CTime > newestAny.CTime {
			newestAny = row
			sawAny = true
		}
		if loggedArchive != "" && path.Base(row.VolID) == loggedArchive {
			logMatch = &rows[i]
		}
		if row.CTime >= uint64(started.Unix()) && (!found || row.CTime > latest.CTime) {
			latest = row
			found = true
		}
	}
	// P1-3: when the task log names a specific archive, the inventory must
	// list that exact file. Falling back to another archive from the time
	// window would transfer a file vzdump did not report writing — the
	// fallback is only for logs that are unreadable or name no archive.
	if loggedArchive != "" {
		if logMatch == nil {
			detail := fmt.Sprintf("backup task log names archive %s but the storage inventory does not list it; refusing to guess another archive; re-run with --archive <volid> to continue from a verified archive", loggedArchive)
			return "", failedStage("backup", detail)
		}
		return logMatch.VolID, output.WorkflowStageResult{Stage: "backup", Status: "succeeded", Task: backup.Task, Detail: logMatch.VolID}
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

// vzdumpArchiveMarker matches the log line vzdump writes when it starts
// writing the archive file.
var vzdumpArchiveMarker = regexp.MustCompile(`(?i)creating vzdump archive ['"]([^'"]+)['"]`)

// archiveNameFromTaskLog reads the finished backup task's log and returns
// the archive file name vzdump reported creating; "" when the log is
// unavailable or names no archive. Only the base name is compared, so the
// host-side directory layout stays irrelevant.
func archiveNameFromTaskLog(ctx context.Context, backend BackupReader, upid string) string {
	reader, ok := backend.(taskLogReader)
	if !ok || strings.TrimSpace(upid) == "" {
		return ""
	}
	info, err := ParseUPID(upid)
	if err != nil {
		return ""
	}
	var rows []output.TaskLogRow
	for start, pages := 0, 0; pages < taskLogMaxPages; start, pages = start+taskLogPageSize, pages+1 {
		page, err := reader.TaskLog(ctx, info.Node, upid, TaskLogPage{Start: start, Limit: taskLogPageSize})
		if err != nil {
			return ""
		}
		rows = append(rows, page...)
		if len(page) < taskLogPageSize {
			break
		}
	}
	for _, row := range rows {
		if match := vzdumpArchiveMarker.FindStringSubmatch(row.Text); match != nil {
			return path.Base(strings.TrimSpace(match[1]))
		}
	}
	return ""
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
