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
				Size: uint64(len(transferPayload)), Format: "tar.zst", CTime: 1710000000,
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
	sourceSSH := newScriptedRunner().
		on("cat /var/lib/vz/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 0, Stdout: transferPayload})
	targetSSH := newScriptedRunner().
		on("test -e /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 1}).
		on("stat -c %s /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part", ssh.Result{ExitCode: 1}).
		on("sha256sum /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part", ssh.Result{ExitCode: 0, Stdout: checksum + "  f\n"})

	sourceWithDirs := &transferTestAPI{fakeBackend: source, dirPaths: map[string]string{"local": "/var/lib/vz"}}
	targetWithDirs := &transferTestAPI{fakeBackend: target, dirPaths: map[string]string{"backup-dst": "/mnt/backups"}}

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

// The full pipeline records every stage independently.
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
	for _, name := range []string{"backup", "transfer", "restore", "post-check"} {
		if _, ok := stages[name]; !ok {
			t.Fatalf("missing stage %q: %+v", name, result.Stages)
		}
		if stages[name].Status != "succeeded" {
			t.Fatalf("stage %s = %+v", name, stages[name])
		}
	}
	if result.Status != "succeeded" {
		t.Fatalf("status = %q", result.Status)
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

// An existing target guest skips backup/transfer/restore and only
// validates — the documented resume semantics for completed workflows.
func TestBackupCopyRestoreWorkflowResumesAtExistingTarget(t *testing.T) {
	workflow, _, target, _, _ := workflowHarness(t)
	target.lxcs["pve8845"] = map[int]*fakeGuest{201: {
		row: output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve8845", Status: "stopped"},
	}}

	result, err := workflow.Run(context.Background(), workflowOptions())
	if err != nil {
		t.Fatalf("workflow: %v", err)
	}
	stages := map[string]output.WorkflowStageResult{}
	for _, stage := range result.Stages {
		stages[stage.Stage] = stage
	}
	for _, name := range []string{"backup", "transfer", "restore"} {
		if stages[name].Status != "skipped" {
			t.Fatalf("stage %s = %+v, want skipped", name, stages[name])
		}
	}
	if stages["post-check"].Status != "succeeded" {
		t.Fatalf("post-check = %+v", stages["post-check"])
	}
}

// A failing stage stops the workflow; earlier stages keep their results and
// nothing rolls back automatically.
func TestBackupCopyRestoreWorkflowStopsOnFailure(t *testing.T) {
	workflow, _, target, _, targetSSH := workflowHarness(t)
	// The transfer fails: the target file already exists (conflict).
	targetSSH.on("test -e /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst", ssh.Result{ExitCode: 0})
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
