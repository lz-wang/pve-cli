package cmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
)

// P1-1: `lxc restore --bridge-map` without `--wait` must fail fast with the
// argument-error exit code before any runtime is built: no backend, no
// connection, no submitted restore task, no structured result on stdout.
func TestRestoreCommandRejectsBridgeMapWithoutWaitBeforeSubmission(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := Dependencies{Stdout: &stdout, Stderr: &stderr}
	deps.BackendFactory = func(config.Profile, pve.ClientOptions) (pve.Backend, error) {
		t.Error("backend must not be built for a rejected argument combination")
		return nil, errors.New("unreachable")
	}

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "restore", "local:backup/example.tar.zst",
		"--node", "pve1", "--vmid", "201",
		"--bridge-map", "vmbr2=vmbr0",
	}, "test", deps)
	if err == nil {
		t.Fatal("expected --bridge-map without --wait to fail the command")
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit code = %d, want 2 (err %v)", code, err)
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want no structured result", stdout.String())
	}
}
