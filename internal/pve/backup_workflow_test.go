package pve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/ssh"
)

const workflowArchive = "local:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst"

func workflowHarness(t *testing.T) (*BackupCopyRestoreWorkflow, *fakeBackend, *fakeBackend, *scriptedRunner, *scriptedRunner) {
	t.Helper()
	source := &fakeBackend{
		nodes: []output.NodeRow{{Name: "n150", Status: "online"}},
		lxcs: map[string]map[int]*fakeGuest{"n150": {101: {
			row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "n150", Name: "app", Status: "stopped"},
		}}},
		backupRows: map[string]map[string][]output.BackupRow{
			"n150": {"local": {{
				Node: "n150", Storage: "local", Kind: "lxc", VMID: 101, VolID: workflowArchive,
				Size: uint64(len(transferPayload)), Format: "tar.zst", CTime: uint64(time.Now().Unix()),
			}}},
		},
	}
	target := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve8845", Status: "online"}},
		lxcs:  map[string]map[int]*fakeGuest{"pve8845": {}},
		storageRows: map[string][]output.StorageRow{
			"pve8845": {{Node: "pve8845", Storage: "backup-dst", Type: "dir", Active: true, Enabled: true, Content: "backup", Avail: 1 << 30}},
		},
		backupRows: map[string]map[string][]output.BackupRow{
			// PVE indexes transferred dir-storage archives automatically.
			"pve8845": {"backup-dst": {{
				Node: "pve8845", Storage: "backup-dst", Kind: "lxc", VMID: 101,
				VolID: "backup-dst:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst",
				Size:  uint64(len(transferPayload)), Format: "tar.zst", CTime: 1710000000,
			}}},
		},
		networkRows: map[string][]output.NetworkRow{
			"pve8845": {{Node: "pve8845", Name: "vmbr0", Type: "bridge", Active: true}},
		},
	}

	sum := sha256.Sum256([]byte(transferPayload))
	checksum := hex.EncodeToString(sum[:])
	const workflowTargetVolid = "backup-dst:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst"
	sourceSSH := newScriptedRunner().
		on("pvesm path "+workflowArchive, ssh.Result{ExitCode: 0, Stdout: "/var/lib/vz/dump/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst\n"}).
		on("cat /var/lib/vz/dump/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 0, Stdout: transferPayload})
	targetSSH := newScriptedRunner().
		on("pvesm path "+workflowTargetVolid, ssh.Result{ExitCode: 0, Stdout: "/mnt/backups/dump/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst\n"}).
		on("test -e /mnt/backups/dump/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 1}).
		on("stat -c %s /mnt/backups/dump/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part", ssh.Result{ExitCode: 1}).
		on("sha256sum /mnt/backups/dump/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part", ssh.Result{ExitCode: 0, Stdout: checksum + "  f\n"}).
		on("pvesm list backup-dst --content backup", ssh.Result{ExitCode: 0, Stdout: workflowTargetVolid + " tar.zst backup 123\n"})

	sourceWithDirs := &transferTestAPI{fakeBackend: source, verify: map[string]error{"local": nil}}
	targetWithDirs := &transferTestAPI{fakeBackend: target, verify: map[string]error{"backup-dst": nil}}

	transfer := NewBackupTransferService(sourceWithDirs, "n150", sourceSSH, targetWithDirs, targetSSH, nil)
	backupRunner := TaskRunner{Wait: true, WaitTimeout: time.Second}
	backupSvc := NewBackupService(source, backupRunner)
	// PVE vzdump is an async task: the mock mirrors the real SDK contract of
	// a non-nil task handle on a successful submission.
	source.lxcs["n150"][101].task = &fakeTask{upid: "UPID:n150:vzdump:101:"}
	source.backupTask = &fakeTask{upid: "UPID:n150:vzdump:101:"}

	target.restoreTask = &fakeTask{upid: "UPID:pve8845:restore"}
	restoreBackend := &restoreWorkflowBackend{fakeBackend: target}
	restoreSvc := NewRestoreService(restoreBackend, TaskRunner{Wait: true, WaitTimeout: time.Second})
	sourceGuests := NewLXCService(source, TaskRunner{}, nil, false)

	workflow := NewBackupCopyRestoreWorkflow(backupSvc, transfer, restoreSvc, sourceGuests, target)
	return workflow, source, target, sourceSSH, targetSSH
}

type restoreWorkflowBackend struct {
	*fakeBackend
}

// Restore simulates PVE: after the restore task, the target guest exists.
func (b *restoreWorkflowBackend) Restore(ctx context.Context, options RestoreOptions) (Task, error) {
	task, err := b.fakeBackend.Restore(ctx, options)
	if err == nil {
		if b.lxcs[options.Node] == nil {
			b.lxcs[options.Node] = map[int]*fakeGuest{}
		}
		if b.lxcs[options.Node][options.VMID] == nil {
			b.lxcs[options.Node][options.VMID] = &fakeGuest{row: output.GuestRow{
				Kind: "lxc", VMID: uint64(options.VMID), Node: options.Node, Status: "stopped",
			}}
		}
	}
	return task, err
}

func workflowOptions() BackupCopyRestoreOptions {
	return BackupCopyRestoreOptions{
		VMID:          101,
		BackupStorage: "local",
		TargetStorage: "backup-dst",
		TargetNode:    "pve8845",
		TargetVMID:    201,
	}
}

// The full pipeline records every stage independently. The post-check
// always carries unconfirmed risks (MAC/IP and DHCP are not observable via
// the API), so the overall result is warning — never an unconditional
// "safe to use" succeeded for an unattended agent.
func TestBackupCopyRestoreWorkflowAllStages(t *testing.T) {
	workflow, source, target, _, _ := workflowHarness(t)

	result, err := workflow.Run(context.Background(), workflowOptions())
	if err != nil {
		t.Fatalf("workflow: %v", err)
	}
	stages := map[string]output.WorkflowStageResult{}
	for _, stage := range result.Stages {
		stages[stage.Stage] = stage
	}
	for _, name := range []string{"backup", "transfer", "restore"} {
		if _, ok := stages[name]; !ok {
			t.Fatalf("missing stage %q: %+v", name, result.Stages)
		}
		if stages[name].Status != "succeeded" {
			t.Fatalf("stage %s = %+v", name, stages[name])
		}
	}
	if stages["post-check"].Status != "warning" {
		t.Fatalf("post-check = %+v, want warning (unconfirmed MAC/IP risks)", stages["post-check"])
	}
	if result.Status != "warning" {
		t.Fatalf("status = %q, want warning — the target must not be reported unconditionally safe", result.Status)
	}
	if result.Archive != workflowArchive {
		t.Fatalf("archive = %q", result.Archive)
	}
	_ = source
	_ = target
}

// An explicit archive skips the backup stage.
func TestBackupCopyRestoreWorkflowSkipsBackupWithArchive(t *testing.T) {
	workflow, _, _, _, _ := workflowHarness(t)
	options := workflowOptions()
	options.Archive = workflowArchive

	result, err := workflow.Run(context.Background(), options)
	if err != nil {
		t.Fatalf("workflow: %v", err)
	}
	stages := map[string]output.WorkflowStageResult{}
	for _, stage := range result.Stages {
		stages[stage.Stage] = stage
	}
	if stages["backup"].Status != "skipped" {
		t.Fatalf("backup stage = %+v, want skipped", stages["backup"])
	}
	if stages["transfer"].Status != "succeeded" {
		t.Fatalf("transfer = %+v", stages["transfer"])
	}
}

// Documented continuation (test matrix): the process died after the backup
// transfer completed but before the restore started. Re-running with
// --archive verifies the already-published file by checksum and continues
// to the restore stage instead of refusing on the existing file.
func TestBackupCopyRestoreWorkflowContinuesFromArchiveAfterTransfer(t *testing.T) {
	workflow, _, _, sourceSSH, targetSSH := workflowHarness(t)
	checksum := dataSHA(transferPayload)
	targetSSH.on("test -e /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 0})
	targetSSH.on("sha256sum /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 0, Stdout: checksum + "  f\n"})
	sourceSSH.on("sha256sum /var/lib/vz/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 0, Stdout: checksum + "  f\n"})

	options := workflowOptions()
	options.Archive = workflowArchive

	result, err := workflow.Run(context.Background(), options)
	if err != nil {
		t.Fatalf("workflow continuation: %v", err)
	}
	stages := map[string]output.WorkflowStageResult{}
	for _, stage := range result.Stages {
		stages[stage.Stage] = stage
	}
	if stages["backup"].Status != "skipped" {
		t.Fatalf("backup stage = %+v, want skipped", stages["backup"])
	}
	if stages["transfer"].Status != "succeeded" {
		t.Fatalf("transfer = %+v, want an idempotent verified continuation", stages["transfer"])
	}
	if stages["restore"].Status != "succeeded" {
		t.Fatalf("restore = %+v, want the continuation to reach the restore stage", stages["restore"])
	}
	if result.Status == "failed" {
		t.Fatalf("status = %q", result.Status)
	}
}

// P0-3: an existing target guest is never treated as a restored resource —
// the same VMID does not prove it originated from this workflow. The
// workflow refuses up front (no auto-resume) instead of validating an
// unrelated container and calling it a success.
func TestBackupCopyRestoreWorkflowRejectsExistingTarget(t *testing.T) {
	workflow, source, target, sourceSSH, targetSSH := workflowHarness(t)
	// The target guest may be completely unrelated to this workflow.
	target.lxcs["pve8845"] = map[int]*fakeGuest{201: {
		row: output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve8845", Status: "stopped"},
	}}

	result, err := workflow.Run(context.Background(), workflowOptions())
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	for _, stage := range result.Stages {
		if stage.Status == "succeeded" {
			t.Fatalf("stage %s succeeded on a refused workflow: %+v", stage.Stage, stage)
		}
	}
	if source.backupCalls != 0 {
		t.Fatalf("backup ran %d times on a refused workflow", source.backupCalls)
	}
	if sourceSSH.saw("cat") || targetSSH.saw("test -e") {
		t.Fatal("transfer ran on a refused workflow")
	}
}

// P1-1 ruling applied to the workflow: an unreadable target inventory must
// fail the run closed — it never reads as "target absent, proceed".
type workflowInventoryBackend struct {
	*fakeBackend
	nodesErr error
	lxcErr   error
}

func (b *workflowInventoryBackend) Nodes(context.Context) ([]output.NodeRow, error) {
	if b.nodesErr != nil {
		return nil, b.nodesErr
	}
	return b.fakeBackend.Nodes(context.Background())
}

func (b *workflowInventoryBackend) LXC(ctx context.Context, node string, vmid int) (Guest, error) {
	if b.lxcErr != nil {
		return nil, b.lxcErr
	}
	return b.fakeBackend.LXC(ctx, node, vmid)
}

func TestBackupCopyRestoreWorkflowFailsClosedOnTargetInventoryError(t *testing.T) {
	cases := []struct {
		name     string
		nodesErr error
		lxcErr   error
	}{
		{"node enumeration fails", errors.New("connection refused"), nil},
		{"guest query fails", nil, errors.New("connection refused")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			workflow, source, target, sourceSSH, _ := workflowHarness(t)
			wrapped := &workflowInventoryBackend{fakeBackend: target, nodesErr: tc.nodesErr, lxcErr: tc.lxcErr}
			closer := NewBackupCopyRestoreWorkflow(workflow.backups, workflow.transfers, workflow.restores, workflow.guests, wrapped)

			result, err := closer.Run(context.Background(), workflowOptions())
			if err == nil {
				t.Fatalf("workflow proceeded on a broken inventory (status %q)", result.Status)
			}
			if result.Status != "failed" {
				t.Fatalf("status = %q, want failed", result.Status)
			}
			if source.backupCalls != 0 {
				t.Fatalf("backup ran on an unverifiable target: %d calls", source.backupCalls)
			}
			if sourceSSH.saw("cat") {
				t.Fatal("transfer ran on an unverifiable target")
			}
		})
	}
}

// P1-7: the workflow must only continue from an archive created by THIS
// run — picking the newest listed archive proves nothing about its origin.
func TestBackupCopyRestoreWorkflowRejectsStaleArchive(t *testing.T) {
	workflow, _, _, sourceSSH, targetSSH := workflowHarness(t)
	// The listing only offers an archive created long before this run.
	reader := workflow.backups.backend.(*fakeBackend)
	reader.backupRows["n150"]["local"] = []output.BackupRow{{
		Node: "n150", Storage: "local", Kind: "lxc", VMID: 101, VolID: workflowArchive,
		Size: uint64(len(transferPayload)), Format: "tar.zst", CTime: 1710000000,
	}}

	result, err := workflow.Run(context.Background(), workflowOptions())
	if err == nil {
		t.Fatal("stale archive must not silently pass as this run's backup")
	}
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	stages := map[string]output.WorkflowStageResult{}
	for _, stage := range result.Stages {
		stages[stage.Stage] = stage
	}
	if stages["backup"].Status != "failed" {
		t.Fatalf("backup stage = %+v, want failed", stages["backup"])
	}
	if !strings.Contains(stages["backup"].Detail, "--archive") {
		t.Fatalf("backup detail = %q, want an exact --archive continuation hint", stages["backup"].Detail)
	}
	if sourceSSH.saw("cat") || targetSSH.saw("test -e") {
		t.Fatal("transfer ran from an uncorrelated archive")
	}
}

// P1-3: a succeeded restore task with a failed post-restore configuration
// must not be reported as a fully succeeded restore stage. The stage reads
// partial, the workflow fails, and the repair responsibility is named.
func TestBackupCopyRestoreWorkflowReportsPartialRestore(t *testing.T) {
	workflow, _, _, _, targetSSH := workflowHarness(t)
	options := workflowOptions()
	// The bridge mapping applies after the restore task; the workflow's
	// restore backend lacks native config access, so the mapping fails
	// while the restore task itself completed.
	options.BridgeMap = map[string]string{"vmbr0": "vmbr0"}

	result, err := workflow.Run(context.Background(), options)
	if err == nil {
		t.Fatal("post-restore configuration failure must fail the workflow")
	}
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	var restore output.WorkflowStageResult
	found := false
	for _, stage := range result.Stages {
		if stage.Stage == "restore" {
			restore = stage
			found = true
		}
	}
	if !found {
		t.Fatalf("restore stage missing: %+v", result.Stages)
	}
	if restore.Status != "partial" {
		t.Fatalf("restore stage = %+v, want partial (task succeeded, configuration failed)", restore)
	}
	if restore.Task == "" {
		t.Fatalf("restore stage must keep the succeeded task UPID: %+v", restore)
	}
	if !strings.Contains(restore.Detail, "bridge") {
		t.Fatalf("restore detail = %q, want the failing configuration step", restore.Detail)
	}
	if targetSSH.saw("umount") {
		t.Fatal("unexpected host interaction")
	}
}

// The preflight refusal covers qemu VMs too: a VMID occupied by a VM is
// still an existing unrelated guest, and refusing late (after backup and
// transfer) would waste the whole pipeline.
func TestBackupCopyRestoreWorkflowRejectsExistingVMTarget(t *testing.T) {
	workflow, source, target, sourceSSH, targetSSH := workflowHarness(t)
	target.vms = map[string]map[int]*fakeGuest{"pve8845": {201: {
		row: output.GuestRow{Kind: "vm", VMID: 201, Node: "pve8845", Status: "stopped"},
	}}}

	result, err := workflow.Run(context.Background(), workflowOptions())
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict for a VM-occupied target VMID", err)
	}
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if source.backupCalls != 0 {
		t.Fatalf("backup ran on a refused workflow: %d calls", source.backupCalls)
	}
	if sourceSSH.saw("cat") || targetSSH.saw("test -e") {
		t.Fatal("transfer ran on a refused workflow")
	}
}

// A failing stage stops the workflow; earlier stages keep their results and
// nothing rolls back automatically.
func TestBackupCopyRestoreWorkflowStopsOnFailure(t *testing.T) {
	workflow, _, target, _, targetSSH := workflowHarness(t)
	// The transfer fails: the target file already exists (conflict).
	targetSSH.on("test -e /mnt/backups/dump/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 0})
	_ = target

	result, err := workflow.Run(context.Background(), workflowOptions())
	if err == nil {
		t.Fatal("expected workflow failure")
	}
	if result.Status != "failed" {
		t.Fatalf("status = %q", result.Status)
	}
	stages := map[string]output.WorkflowStageResult{}
	for _, stage := range result.Stages {
		stages[stage.Stage] = stage
	}
	if stages["backup"].Status != "succeeded" {
		t.Fatalf("backup = %+v", stages["backup"])
	}
	if stages["transfer"].Status != "failed" {
		t.Fatalf("transfer = %+v", stages["transfer"])
	}
	if _, exists := stages["restore"]; exists {
		t.Fatalf("restore must not run after a failed transfer: %+v", stages["restore"])
	}
	found := false
	for _, stage := range result.Stages {
		if stage.Stage == "post-check" {
			found = true
		}
	}
	if found {
		t.Fatal("post-check must not run after a failure")
	}
}

func TestBackupCopyRestoreWorkflowValidations(t *testing.T) {
	workflow, _, _, _, _ := workflowHarness(t)
	if _, err := workflow.Run(context.Background(), BackupCopyRestoreOptions{VMID: 0}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if _, err := workflow.Run(context.Background(), BackupCopyRestoreOptions{VMID: 101}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want missing backup storage or archive", err)
	}
	if _, err := workflow.Run(context.Background(), BackupCopyRestoreOptions{VMID: 101, BackupStorage: "local"}); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "target-storage") {
		t.Fatalf("err = %v", err)
	}
}
