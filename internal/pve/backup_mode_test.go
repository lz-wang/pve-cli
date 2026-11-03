package pve

import (
	"context"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

func TestParseEffectiveBackupMode(t *testing.T) {
	cases := []struct {
		name  string
		lines []output.TaskLogRow
		mode  string
		found bool
	}{
		{
			name: "starting backup mode line",
			lines: []output.TaskLogRow{
				{Line: 1, Text: "INFO: Starting Backup of VM 101 (qemu)"},
				{Line: 2, Text: "INFO: starting backup mode: snapshot"},
				{Line: 3, Text: "INFO: creating vzdump archive"},
			},
			mode:  "snapshot",
			found: true,
		},
		{
			name: "suspend mode line",
			lines: []output.TaskLogRow{
				{Line: 1, Text: "INFO: starting backup mode: suspend"},
			},
			mode:  "suspend",
			found: true,
		},
		{
			name: "last mode marker wins after fallback",
			lines: []output.TaskLogRow{
				{Line: 1, Text: "INFO: starting backup mode: snapshot"},
				{Line: 2, Text: "INFO: backup mode 'snapshot' failed - fall back to 'suspend'"},
				{Line: 3, Text: "INFO: starting backup mode: suspend"},
			},
			mode:  "suspend",
			found: true,
		},
		{
			name: "quoted fallback marker",
			lines: []output.TaskLogRow{
				{Line: 1, Text: "INFO: mode snapshot failed, falling back to stop mode"},
			},
			mode:  "stop",
			found: true,
		},
		{
			name: "no mode marker",
			lines: []output.TaskLogRow{
				{Line: 1, Text: "INFO: Starting Backup of VM 101 (qemu)"},
				{Line: 2, Text: "INFO: 12% (3.2 GiB of 24.0 GiB)"},
			},
			found: false,
		},
		{
			name:  "empty log",
			lines: nil,
			found: false,
		},
		{
			name: "unrelated words are not markers",
			lines: []output.TaskLogRow{
				{Line: 1, Text: "INFO: suspend guest for snapshot"},
			},
			found: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, found := parseEffectiveBackupMode(tc.lines)
			if found != tc.found || (found && mode != tc.mode) {
				t.Fatalf("mode = %q found = %v, want %q/%v", mode, found, tc.mode, tc.found)
			}
		})
	}
}

func backupModeBackend(task Task, logPages [][]output.TaskLogRow) *fakeBackend {
	backend := &fakeBackend{
		nodes:      []output.NodeRow{{Name: "pve1"}},
		vms:        map[string]map[int]*fakeGuest{"pve1": {100: {row: output.GuestRow{Kind: "vm", VMID: 100, Node: "pve1"}}}},
		backupTask: task,
	}
	if logPages != nil {
		backend.taskLogPages = map[string][][]output.TaskLogRow{task.UPID(): logPages}
	}
	return backend
}

const backupModeTestUPID = "UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam:"

// A waited, completed backup with a readable task log reports the mode the
// task actually used, sourced from the log.
func TestBackupReportsEffectiveModeFromTaskLog(t *testing.T) {
	task := &fakeTask{upid: backupModeTestUPID}
	backend := backupModeBackend(task, [][]output.TaskLogRow{{
		{Line: 1, Text: "INFO: Starting Backup of VM 100 (lxc)"},
		{Line: 2, Text: "INFO: starting backup mode: suspend"},
	}})
	svc := NewBackupService(backend, TaskRunner{Wait: true})

	result, err := svc.BackupGuest(context.Background(), BackupCreateOptions{
		Kind: BackupKindVM, VMID: 100, Node: "pve1", Storage: "backup",
	})
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if result.RequestedMode != BackupModeSnapshot || result.Mode != BackupModeSnapshot {
		t.Fatalf("requested mode = %q legacy mode = %q", result.RequestedMode, result.Mode)
	}
	if result.EffectiveMode != BackupModeSuspend {
		t.Fatalf("effective mode = %q, want suspend", result.EffectiveMode)
	}
	if result.EffectiveModeSource != "task_log" {
		t.Fatalf("source = %q", result.EffectiveModeSource)
	}
}

// Without --wait the task has not finished; the effective mode stays
// unknown and is never inferred from the request or the submission.
func TestBackupEffectiveModeUnknownWithoutWait(t *testing.T) {
	task := &fakeTask{upid: backupModeTestUPID}
	backend := backupModeBackend(task, [][]output.TaskLogRow{{
		{Line: 1, Text: "INFO: starting backup mode: stop"},
	}})
	svc := NewBackupService(backend, TaskRunner{})

	result, err := svc.BackupGuest(context.Background(), BackupCreateOptions{
		Kind: BackupKindVM, VMID: 100, Node: "pve1", Storage: "backup", Mode: BackupModeSuspend,
	})
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if result.Status != "submitted" {
		t.Fatalf("status = %q", result.Status)
	}
	if result.RequestedMode != BackupModeSuspend {
		t.Fatalf("requested = %q", result.RequestedMode)
	}
	if result.EffectiveMode != "unknown" || result.EffectiveModeSource != "" {
		t.Fatalf("effective = %q source = %q, want unknown/empty", result.EffectiveMode, result.EffectiveModeSource)
	}
}

// A completed task whose log has no reliable marker reports unknown; success
// alone never implies the requested mode ran.
func TestBackupEffectiveModeUnknownWhenLogHasNoMarker(t *testing.T) {
	task := &fakeTask{upid: backupModeTestUPID}
	backend := backupModeBackend(task, [][]output.TaskLogRow{{
		{Line: 1, Text: "INFO: Starting Backup of VM 100 (qemu)"},
	}})
	svc := NewBackupService(backend, TaskRunner{Wait: true})

	result, err := svc.BackupGuest(context.Background(), BackupCreateOptions{
		Kind: BackupKindVM, VMID: 100, Node: "pve1", Storage: "backup",
	})
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if result.EffectiveMode != "unknown" || result.EffectiveModeSource != "" {
		t.Fatalf("effective = %q source = %q", result.EffectiveMode, result.EffectiveModeSource)
	}
}

// Log read failures leave the mode unknown without failing the backup.
type failingLogBackend struct {
	*fakeBackend
}

func (b *failingLogBackend) TaskLog(context.Context, string, string, TaskLogPage) ([]output.TaskLogRow, error) {
	return nil, ErrUnavailable
}

func TestBackupEffectiveModeUnknownWhenLogUnreadable(t *testing.T) {
	task := &fakeTask{upid: backupModeTestUPID}
	inner := backupModeBackend(task, nil)
	backend := &failingLogBackend{fakeBackend: inner}
	svc := NewBackupService(backend, TaskRunner{Wait: true})

	result, err := svc.BackupGuest(context.Background(), BackupCreateOptions{
		Kind: BackupKindVM, VMID: 100, Node: "pve1", Storage: "backup",
	})
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if result.Status != "succeeded" || result.EffectiveMode != "unknown" {
		t.Fatalf("result = %+v", result)
	}
}

// Failed tasks report unknown effective mode.
func TestBackupEffectiveModeUnknownOnFailedTask(t *testing.T) {
	task := &fakeTask{upid: backupModeTestUPID, failed: true, exitStatus: "ERROR"}
	backend := backupModeBackend(task, [][]output.TaskLogRow{{
		{Line: 1, Text: "INFO: starting backup mode: suspend"},
	}})
	svc := NewBackupService(backend, TaskRunner{Wait: true})

	result, err := svc.BackupGuest(context.Background(), BackupCreateOptions{
		Kind: BackupKindVM, VMID: 100, Node: "pve1", Storage: "backup",
	})
	if err == nil {
		t.Fatal("expected task failure")
	}
	if result.EffectiveMode != "unknown" {
		t.Fatalf("effective = %q, want unknown", result.EffectiveMode)
	}
}
