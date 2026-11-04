package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

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
				Size: uint64(len(copyPayload)), Format: "tar.zst", CTime: uint64(time.Now().Unix()),
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
				Size:  uint64(len(copyPayload)), Format: "tar.zst", CTime: uint64(time.Now().Unix()),
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

// P1-4: an unconfirmed mount outcome fails the command (exit 5) while the
// structured result keeps status=unknown.
func TestLXCMountCommandUnknownOutcomeExitFive(t *testing.T) {
	row := output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve8845", Status: "stopped"}
	runner := &mountCmdRunner{sequenced: map[string][]ssh.Result{
		"mountpoint -q /var/lib/lxc/101/rootfs": {
			{ExitCode: 1}, // pre: not mounted
			{ExitCode: 1}, // post: still not mounted -> unconfirmed
		},
	}, defaults: ssh.Result{ExitCode: 0, Stdout: "filesystem is mounted successfully at /var/lib/lxc/101/rootfs\n"}}
	var stdout, stderr bytes.Buffer
	deps := mountCmdDeps(runner, row, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeSSHTestConfig(t, "pve8845"),
		"lxc", "mount", "101", "--node", "pve8845", "-o", "json",
	}, "test", deps)
	if !errors.Is(err, pve.ErrUncertain) {
		t.Fatalf("err = %v, want ErrUncertain", err)
	}
	if code := ExitCode(err); code != 5 {
		t.Fatalf("exit = %d, want 5", code)
	}
	if !strings.Contains(stdout.String(), `"status": "unknown"`) {
		t.Fatalf("stdout must keep the unknown status: %s", stdout.String())
	}
}

// P0-3: an existing target VMID is refused up front — it is not proof that
// a previous run created it.
func TestBackupCopyRestoreCommandRejectsExistingTarget(t *testing.T) {
	lxcRow := output.GuestRow{Kind: "lxc", VMID: 101, Name: "app", Node: "n150", Status: "stopped"}
	source := &cmdWorkflowBackend{commandBackend: &commandBackend{
		nodes:     []output.NodeRow{{Name: "n150", Status: "online"}},
		lxcs:      map[string][]output.GuestRow{"n150": {lxcRow}},
		lxcGuests: map[string]map[int]*commandGuest{"n150": {101: {row: lxcRow}}},
		backups: map[string]map[string][]output.BackupRow{
			"n150": {"local": {{
				Node: "n150", Storage: "local", Kind: "lxc", VMID: 101, VolID: copyVolid,
				Size: uint64(len(copyPayload)), Format: "tar.zst", CTime: uint64(time.Now().Unix()),
			}}},
		},
		dirPaths:   map[string]string{"local": "/var/lib/vz"},
		backupTask: &commandTask{upid: "UPID:n150:vzdump:101:root@pam:"},
	}}
	target := &cmdWorkflowBackend{commandBackend: &commandBackend{
		nodes:     []output.NodeRow{{Name: "pve8845", Status: "online"}},
		lxcs:      map[string][]output.GuestRow{"pve8845": {{Kind: "lxc", VMID: 201, Node: "pve8845", Status: "stopped"}}},
		lxcGuests: map[string]map[int]*commandGuest{"pve8845": {201: {row: output.GuestRow{Kind: "lxc", VMID: 201, Node: "pve8845", Status: "stopped"}}}},
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
		"--wait", "-o", "json",
	}, "test", deps)
	if !errors.Is(err, pve.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if code := ExitCode(err); code != 4 {
		t.Fatalf("exit = %d, want 4", code)
	}
	if source.backupCalls != 0 {
		t.Fatalf("backup ran on a refused workflow: %d calls", source.backupCalls)
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

// Review I-1/I-2: --node overrides the API-resolved source node, and an
// ssh.node bound to a different node refuses before any host command.
func TestBackupCopyRestoreCommandAcceptsNodeOverride(t *testing.T) {
	lxcRow := output.GuestRow{Kind: "lxc", VMID: 101, Name: "app", Node: "n150", Status: "stopped"}
	source := &cmdWorkflowBackend{commandBackend: &commandBackend{
		nodes:     []output.NodeRow{{Name: "n150", Status: "online"}},
		lxcs:      map[string][]output.GuestRow{"n150": {lxcRow}},
		lxcGuests: map[string]map[int]*commandGuest{"n150": {101: {row: lxcRow}}},
		backups: map[string]map[string][]output.BackupRow{
			"n150": {"local": {{
				Node: "n150", Storage: "local", Kind: "lxc", VMID: 101, VolID: copyVolid,
				Size: uint64(len(copyPayload)), Format: "tar.zst", CTime: uint64(time.Now().Unix()),
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
				Size:  uint64(len(copyPayload)), Format: "tar.zst", CTime: uint64(time.Now().Unix()),
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
		"--node", "n150",
		"--wait", "-o", "json",
	}, "test", deps)
	if err != nil {
		t.Fatalf("copy-restore with --node: %v (stderr %s)", err, stderr.String())
	}
	if source.backupCalls != 1 {
		t.Fatalf("backup calls = %d, want 1", source.backupCalls)
	}
}

func TestBackupCopyRestoreCommandRejectsSSHNodeMismatch(t *testing.T) {
	lxcRow := output.GuestRow{Kind: "lxc", VMID: 101, Name: "app", Node: "n150", Status: "stopped"}
	source := &cmdWorkflowBackend{commandBackend: &commandBackend{
		nodes:     []output.NodeRow{{Name: "n150", Status: "online"}},
		lxcs:      map[string][]output.GuestRow{"n150": {lxcRow}},
		lxcGuests: map[string]map[int]*commandGuest{"n150": {101: {row: lxcRow}}},
	}}
	target := &cmdWorkflowBackend{commandBackend: &commandBackend{
		nodes: []output.NodeRow{{Name: "pve8845", Status: "online"}},
	}}
	var stdout, stderr bytes.Buffer
	deps := Dependencies{Stdout: &stdout, Stderr: &stderr}
	deps.BackendFactory = func(profile config.Profile, _ pve.ClientOptions) (pve.Backend, error) {
		if strings.Contains(profile.Endpoint, "pve8845") {
			return target, nil
		}
		return source, nil
	}
	deps.SSHRunnerFactory = func(profile ssh.Profile) pve.CommandRunner {
		if profile.Host == "pve8845.lan" {
			return &cmdScriptedRunner{}
		}
		return &cmdScriptedRunner{}
	}

	cfgPath := t.TempDir() + "/config.yaml"
	t.Setenv("PVE_TOKEN", "secret")
	cfg := config.Empty()
	if err := cfg.SetProfile("n150", config.Profile{
		Endpoint:       "https://n150.lan:8006",
		TokenID:        "root@pam!test",
		TokenSecretEnv: "PVE_TOKEN",
		SSH:            &ssh.Profile{Host: "n150.lan", Node: "pve8845", User: "root"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetProfile("pve8845", config.Profile{
		Endpoint:       "https://pve8845.lan:8006",
		TokenID:        "root@pam!test",
		TokenSecretEnv: "PVE_TOKEN",
		SSH:            &ssh.Profile{Host: "pve8845.lan", Node: "pve8845", User: "root"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath, "--profile", "n150",
		"backup", "copy-restore", "101",
		"--from", "n150", "--to", "pve8845",
		"--backup-storage", "local",
		"--target-storage", "backup-dst",
		"--target-node", "pve8845",
		"--target-vmid", "201",
		"--wait", "-o", "json",
	}, "test", deps)
	if err == nil || !strings.Contains(err.Error(), "bound to PVE node") {
		t.Fatalf("err = %v, want an ssh.node binding refusal", err)
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}
