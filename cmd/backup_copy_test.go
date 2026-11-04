package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/lz-wang/pve-cli/v3/internal/ssh"
)

// cmdScriptedRunner mirrors the pve-package scripted runner for the cmd layer.
type cmdScriptedRunner struct {
	script map[string]ssh.Result
	calls  []string
}

func (r *cmdScriptedRunner) Run(_ context.Context, stdin io.Reader, stdout io.Writer, _ io.Writer, command string, args ...string) (ssh.Result, error) {
	key := strings.Join(append([]string{command}, args...), " ")
	r.calls = append(r.calls, key)
	if stdin != nil {
		done := make(chan struct{})
		go func() { _, _ = io.Copy(io.Discard, stdin); close(done) }()
		<-done
	}
	result := ssh.Result{ExitCode: 0}
	if scripted, ok := r.script[key]; ok {
		result = scripted
	}
	if stdout != nil && result.Stdout != "" {
		if _, err := stdout.Write([]byte(result.Stdout)); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (r *cmdScriptedRunner) saw(part string) bool {
	for _, call := range r.calls {
		if strings.Contains(call, part) {
			return true
		}
	}
	return false
}

func writeTwoSSHProfileConfig(t *testing.T) string {
	t.Helper()
	cfgPath := t.TempDir() + "/config.yaml"
	t.Setenv("PVE_TOKEN", "secret")
	cfg := config.Empty()
	for _, name := range []string{"n150", "pve8845"} {
		if err := cfg.SetProfile(name, config.Profile{
			Endpoint:       "https://" + name + ".lan:8006",
			TokenID:        "root@pam!test",
			TokenSecretEnv: "PVE_TOKEN",
			SSH:            &ssh.Profile{Host: name, User: "root"},
		}); err != nil {
			t.Fatalf("set profile %s: %v", name, err)
		}
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

const copyVolid = "local:backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst"
const copyPayload = "archive-bytes"

func copyTestBackends() (source, target *commandBackend) {
	source = &commandBackend{
		nodes: []output.NodeRow{{Name: "n150", Status: "online"}},
		backups: map[string]map[string][]output.BackupRow{
			"n150": {"local": {{
				Node: "n150", Storage: "local", Kind: "lxc", VMID: 101, VolID: copyVolid,
				Size: uint64(len(copyPayload)), Format: "tar.zst",
			}}},
		},
		dirPaths: map[string]string{"local": "/var/lib/vz"},
	}
	target = &commandBackend{
		nodes:    []output.NodeRow{{Name: "pve8845", Status: "online"}},
		dirPaths: map[string]string{"backup-dst": "/mnt/backups"},
	}
	return source, target
}

func copyDeps(source, target *commandBackend, sourceRunner, targetRunner *cmdScriptedRunner, stdout, stderr *bytes.Buffer) Dependencies {
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

func TestBackupCopyCommandEndToEnd(t *testing.T) {
	source, target := copyTestBackends()
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
	var stdout, stderr bytes.Buffer
	deps := copyDeps(source, target, sourceRunner, targetRunner, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoSSHProfileConfig(t),
		"backup", "copy", copyVolid,
		"--from", "n150", "--to", "pve8845", "--target-storage", "backup-dst",
		"-o", "json",
	}, "test", deps)
	if err != nil {
		t.Fatalf("copy: %v (stderr %s)", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{`"status": "succeeded"`, `"bytes": ` + fmt.Sprint(len(copyPayload)), `"sha256": "` + checksum + `"`, `"target_storage": "backup-dst"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if !targetRunner.saw("dd of=") || !targetRunner.saw("ln /mnt/backups/backup/") || targetRunner.saw("mv ") {
		t.Fatalf("target commands = %v", targetRunner.calls)
	}
}

func TestBackupCopyCommandRequiresSSHProfiles(t *testing.T) {
	source, target := copyTestBackends()
	runner := &cmdScriptedRunner{}
	var stdout, stderr bytes.Buffer
	deps := copyDeps(source, target, runner, runner, &stdout, &stderr)

	// Plain two-profile config without ssh sections.
	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"backup", "copy", copyVolid,
		"--from", "n150", "--to", "pve8845", "--target-storage", "backup-dst",
	}, "test", deps)
	if err == nil || !strings.Contains(err.Error(), "ssh section") {
		t.Fatalf("err = %v", err)
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestBackupCopyCommandRefusesOverwrite(t *testing.T) {
	source, target := copyTestBackends()
	sum := sha256.Sum256([]byte(copyPayload))
	checksum := hex.EncodeToString(sum[:])
	sourceRunner := &cmdScriptedRunner{script: map[string]ssh.Result{
		"sha256sum /var/lib/vz/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst": {ExitCode: 0, Stdout: checksum + "  f\n"},
	}}
	targetRunner := &cmdScriptedRunner{script: map[string]ssh.Result{
		"test -e /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst": {ExitCode: 0},
		"sha256sum /mnt/backups/backup/vzdump-lxc-101-2026_06_06-00_00_00.tar.zst": {ExitCode: 0, Stdout: strings.Repeat("f", 64) + "  f\n"},
	}}
	var stdout, stderr bytes.Buffer
	deps := copyDeps(source, target, sourceRunner, targetRunner, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoSSHProfileConfig(t),
		"backup", "copy", copyVolid,
		"--from", "n150", "--to", "pve8845", "--target-storage", "backup-dst",
	}, "test", deps)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v", err)
	}
	if code := ExitCode(err); code != 4 {
		t.Fatalf("exit = %d, want 4", code)
	}
	if targetRunner.saw("dd of=") {
		t.Fatal("no transfer over an existing file")
	}
}
