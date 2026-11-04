package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/lz-wang/pve-cli/v3/internal/ssh"
)

// cmdWorkflowBackend extends commandBackend with a restore that creates
// the target guest, like PVE does.
type cmdWorkflowBackend struct {
	*commandBackend
}

func (b *cmdWorkflowBackend) Restore(ctx context.Context, options pve.RestoreOptions) (pve.Task, error) {
	task, err := b.commandBackend.Restore(ctx, options)
	if err == nil {
		if b.lxcGuests == nil {
			b.lxcGuests = map[string]map[int]*commandGuest{}
		}
		if b.lxcGuests[options.Node] == nil {
			b.lxcGuests[options.Node] = map[int]*commandGuest{}
		}
		if b.lxcGuests[options.Node][options.VMID] == nil {
			b.lxcGuests[options.Node][options.VMID] = &commandGuest{row: output.GuestRow{
				Kind: "lxc", VMID: uint64(options.VMID), Node: options.Node, Status: "stopped",
			}}
		}
	}
	return task, err
}

func workflowCmdDeps(t *testing.T, source, target *cmdWorkflowBackend, stdout, stderr *bytes.Buffer) Dependencies {
	t.Helper()
	sum := sha256.Sum256([]byte(copyPayload))
	checksum := hex.EncodeToString(sum[:])
	sourceRunner := &cmdScriptedRunner{script: map[string]ssh.Result{
		"cat /var/lib/vz/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst": {ExitCode: 0, Stdout: copyPayload},
	}}
	targetRunner := &cmdScriptedRunner{script: map[string]ssh.Result{
		"test -e /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst":                 {ExitCode: 1},
		"stat -c %s /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part": {ExitCode: 1},
		"sha256sum /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst.pve-cli-part":  {ExitCode: 0, Stdout: checksum + "  f\n"},
	}}
	deps := Dependencies{Stdout: stdout, Stderr: stderr}
	deps.BackendFactory = func(profile config.Profile, _ pve.ClientOptions) (pve.Backend, error) {
		if strings.Contains(profile.Endpoint, "pve8845") {
			return target, nil
		}
		return source, nil
	}
	deps.SSHRunnerFactory = func(profile ssh.Profile) pve.CommandRunner {
		if profile.Host == "pve8845" {
			return targetRunner
		}
		return sourceRunner
	}
	return deps
}

func TestBackupCopyRestoreCommandEndToEnd(t *testing.T) {
	lxcRow := output.GuestRow{Kind: "lxc", VMID: 101, Name: "app", Node: "n150", Status: "stopped"}
	source := &cmdWorkflowBackend{commandBackend: &commandBackend{
		nodes:     []output.NodeRow{{Name: "n150", Status: "online"}},
		lxcs:      map[string][]output.GuestRow{"n150": {lxcRow}},
		lxcGuests: map[string]map[int]*commandGuest{"n150": {101: {row: lxcRow}}},
		backups: map[string]map[string][]output.BackupRow{
			"n150": {"local": {{
				Node: "n150", Storage: "local", Kind: "lxc", VMID: 101, VolID: copyVolid,
				Size: uint64(len(copyPayload)), Format: "tar.zst", CTime: 1710000000,
			}}},
		},
		dirPaths:   map[string]string{"local": "/var/lib/vz"},
		backupTask: &commandTask{upid: "UPID:n150:vzdump:101:root@pam:"},
	}}
	target := &cmdWorkflowBackend{commandBackend: &commandBackend{
		nodes: []output.NodeRow{{Name: "pve8845", Status: "online"}},
		lxcs:  map[string][]output.GuestRow{},
		storages: map[string][]output.StorageRow{
			"pve8845": {{Node: "pve8845", Storage: "backup-dst", Type: "dir", Active: true, Enabled: true, Content: "backup", Avail: 1 << 30}},
		},
		backups: map[string]map[string][]output.BackupRow{
			"pve8845": {"backup-dst": {{
				Node: "pve8845", Storage: "backup-dst", Kind: "lxc", VMID: 101,
				VolID: "backup-dst:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst",
				Size:  uint64(len(copyPayload)), Format: "tar.zst", CTime: 1710000000,
			}}},
		},
		dirPaths:   map[string]string{"backup-dst": "/mnt/backups"},
		backupTask: &commandTask{upid: "UPID:pve8845:restore:201:root@pam:"},
	}}
	var stdout, stderr bytes.Buffer
	deps := workflowCmdDeps(t, source, target, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoSSHProfileConfig(t),
		"backup", "copy-restore", "101",
		"--from", "n150", "--to", "pve8845",
		"--backup-storage", "local",
		"--target-storage", "backup-dst",
		"--target-node", "pve8845",
		"--target-vmid", "201",
		"--wait",
		"-o", "json",
	}, "test", deps)
	if err != nil {
		t.Fatalf("workflow: %v (stderr %s)", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{`"stage": "backup"`, `"stage": "transfer"`, `"stage": "restore"`, `"stage": "post-check"`, `"status": "succeeded"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if target.restoreOptions.VMID != 201 {
		t.Fatalf("restore options = %+v", target.restoreOptions)
	}
}

func TestBackupCopyRestoreCommandRequiresWait(t *testing.T) {
	source := &cmdWorkflowBackend{commandBackend: &commandBackend{}}
	target := &cmdWorkflowBackend{commandBackend: &commandBackend{}}
	var stdout, stderr bytes.Buffer
	deps := workflowCmdDeps(t, source, target, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoSSHProfileConfig(t),
		"backup", "copy-restore", "101",
		"--from", "n150", "--to", "pve8845",
		"--backup-storage", "local",
		"--target-storage", "backup-dst",
		"--target-node", "pve8845",
	}, "test", deps)
	if err == nil || !strings.Contains(err.Error(), "wait") {
		t.Fatalf("err = %v", err)
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}
