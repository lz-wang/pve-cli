package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/lz-wang/pve-cli/v3/internal/ssh"
)

type execTestRunner struct {
	called bool
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	result ssh.Result
	err    error
}

func (r *execTestRunner) Run(_ context.Context, stdin io.Reader, stdout, stderr io.Writer, command string, args ...string) (ssh.Result, error) {
	r.called = true
	r.stdin = stdin
	r.stdout = stdout
	r.stderr = stderr
	return r.result, r.err
}

func execTestDeps(_ *testing.T, stdout, stderr *bytes.Buffer, runner pve.CommandRunner) Dependencies {
	backend := &commandBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		lxcs:  map[string][]output.GuestRow{"pve1": {{Kind: "lxc", VMID: 200, Name: "app", Node: "pve1"}}},
		lxcGuests: map[string]map[int]*commandGuest{
			"pve1": {200: {row: output.GuestRow{Kind: "lxc", VMID: 200, Node: "pve1"}}},
		},
	}
	deps := commandDeps(stdout, stderr, backend)
	deps.SSHRunnerFactory = func(profile ssh.Profile) pve.CommandRunner { return runner }
	return deps
}

func writeSSHTestConfig(t *testing.T, node string) string {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("PVE_TOKEN", "secret")
	cfg := config.Empty()
	if err := cfg.SetProfile("home", config.Profile{
		Endpoint:       "https://pve.example:8006/api2/json",
		TokenID:        "root@pam!test",
		TokenSecretEnv: "PVE_TOKEN",
		SSH:            &ssh.Profile{Host: "pve1", Node: node, User: "root"},
	}); err != nil {
		t.Fatalf("set profile: %v", err)
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return cfgPath
}

func TestLXCExecCommandJSONOutput(t *testing.T) {
	runner := &execTestRunner{result: ssh.Result{ExitCode: 0, Stdout: "active\n", Stderr: ""}}
	var stdout, stderr bytes.Buffer
	deps := execTestDeps(t, &stdout, &stderr, runner)

	err := RunWithDependencies([]string{
		"pve", "--config", writeSSHTestConfig(t, "pve1"),
		"lxc", "exec", "200", "--node", "pve1",
		"-o", "json", "--", "systemctl", "status", "nginx",
	}, "test", deps)
	if err != nil {
		t.Fatalf("exec: %v (stderr %s)", err, stderr.String())
	}
	var result output.AgentExecResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("json: %v\n%s", err, stdout.String())
	}
	if result.ExitCode != 0 || result.Stdout != "active\n" {
		t.Fatalf("result = %+v", result)
	}
	if runner.called && runner.stdout != nil {
		t.Fatal("json mode must not stream to process stdout")
	}
}

// Text mode forwards the process streams as live sinks; no structured
// result is printed.
func TestLXCExecCommandTextModeStreamsThrough(t *testing.T) {
	runner := &execTestRunner{result: ssh.Result{ExitCode: 0, Stdout: "hello", Stderr: "warn"}}
	var stdout, stderr bytes.Buffer
	deps := execTestDeps(t, &stdout, &stderr, runner)

	err := RunWithDependencies([]string{
		"pve", "--config", writeSSHTestConfig(t, "pve1"),
		"lxc", "exec", "200", "--node", "pve1",
		"--", "echo", "hi",
	}, "test", deps)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if runner.stdout == nil || runner.stderr == nil {
		t.Fatal("text mode must pass the process streams as live sinks")
	}
	if strings.Contains(stdout.String(), "exit_code") {
		t.Fatalf("text mode printed structured output: %s", stdout.String())
	}
}

func TestLXCExecCommandPreservesRemoteExitCode(t *testing.T) {
	runner := &execTestRunner{result: ssh.Result{ExitCode: 7, Stderr: "failed"}}
	var stdout, stderr bytes.Buffer
	deps := execTestDeps(t, &stdout, &stderr, runner)

	err := RunWithDependencies([]string{
		"pve", "--config", writeSSHTestConfig(t, "pve1"),
		"lxc", "exec", "200", "-o", "json", "--", "false",
	}, "test", deps)
	var remote *pve.RemoteExitError
	if !errors.As(err, &remote) || remote.Code != 7 {
		t.Fatalf("err = %v, want remote exit 7", err)
	}
	if code := ExitCode(err); code != 7 {
		t.Fatalf("process exit = %d, want 7", code)
	}
	var result output.AgentExecResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("failed run must still emit JSON result: %v\n%s", err, stdout.String())
	}
}

func TestLXCExecCommandConnectFailureExitThree(t *testing.T) {
	runner := &execTestRunner{err: ssh.NewConnectError("ssh connection failed: Connection refused", "Connection refused")}
	var stdout, stderr bytes.Buffer
	deps := execTestDeps(t, &stdout, &stderr, runner)

	err := RunWithDependencies([]string{
		"pve", "--config", writeSSHTestConfig(t, "pve1"),
		"lxc", "exec", "200", "-o", "json", "--", "true",
	}, "test", deps)
	var connect *ssh.ConnectError
	if !errors.As(err, &connect) {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if code := ExitCode(err); code != 3 {
		t.Fatalf("process exit = %d, want 3", code)
	}
}

func TestLXCExecCommandRequiresSSHProfile(t *testing.T) {
	runner := &execTestRunner{}
	var stdout, stderr bytes.Buffer
	deps := execTestDeps(t, &stdout, &stderr, runner)
	// Strip the SSH section by using a plain test config.
	deps2 := Dependencies{BackendFactory: deps.BackendFactory, Stdin: deps.Stdin, Stdout: deps.Stdout, Stderr: deps.Stderr}

	err := RunWithDependencies([]string{
		"pve", "--config", writeTestConfig(t, "json"),
		"lxc", "exec", "200", "--", "true",
	}, "test", deps2)
	if err == nil || !strings.Contains(err.Error(), "ssh section") {
		t.Fatalf("err = %v", err)
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if runner.called {
		t.Fatal("runner must not be invoked without ssh profile")
	}
}

func TestLXCExecCommandForwardsStdin(t *testing.T) {
	runner := &execTestRunner{}
	var stdout, stderr bytes.Buffer
	deps := execTestDeps(t, &stdout, &stderr, runner)
	deps.Stdin = strings.NewReader("payload")

	err := RunWithDependencies([]string{
		"pve", "--config", writeSSHTestConfig(t, "pve1"),
		"lxc", "exec", "200", "--node", "pve1", "--stdin", "--", "cat",
	}, "test", deps)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if runner.stdin == nil {
		t.Fatal("stdin not forwarded")
	}
	data, _ := io.ReadAll(runner.stdin)
	if string(data) != "payload" {
		t.Fatalf("stdin = %q", string(data))
	}
}

// P1-2: the CLI refuses pct commands when the profile's ssh.node disagrees
// with the API-resolved guest node, and when it is not declared at all.
func TestLXCExecCommandRejectsSSHNodeMismatch(t *testing.T) {
	runner := &execTestRunner{}
	var stdout, stderr bytes.Buffer
	deps := execTestDeps(t, &stdout, &stderr, runner) // guest 200 lives on pve1

	err := RunWithDependencies([]string{
		"pve", "--config", writeSSHTestConfig(t, "n150"),
		"lxc", "exec", "200", "--node", "pve1", "--", "true",
	}, "test", deps)
	if err == nil || !strings.Contains(err.Error(), "n150") || !strings.Contains(err.Error(), "pve1") {
		t.Fatalf("err = %v, want a node mismatch refusal", err)
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if runner.called {
		t.Fatal("pct exec must not run on the wrong SSH host")
	}
}

func TestLXCExecCommandRequiresNodeDeclaration(t *testing.T) {
	runner := &execTestRunner{}
	var stdout, stderr bytes.Buffer
	deps := execTestDeps(t, &stdout, &stderr, runner)

	err := RunWithDependencies([]string{
		"pve", "--config", writeSSHTestConfig(t, ""),
		"lxc", "exec", "200", "--node", "pve1", "--", "true",
	}, "test", deps)
	if err == nil || !strings.Contains(err.Error(), "node") {
		t.Fatalf("err = %v, want a node declaration demand", err)
	}
	if runner.called {
		t.Fatal("no pct command without a declared node")
	}
}

func TestLXCExecCommandValidations(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing command", []string{"lxc", "exec", "200"}, "COMMAND"},
		{"missing vmid and command", []string{"lxc", "exec"}, "COMMAND"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &execTestRunner{}
			var stdout, stderr bytes.Buffer
			deps := execTestDeps(t, &stdout, &stderr, runner)
			full := append([]string{"pve", "--config", writeSSHTestConfig(t, "pve1")}, tc.args...)
			err := RunWithDependencies(full, "test", deps)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}
			if code := ExitCode(err); code != 2 {
				t.Fatalf("exit = %d, want 2", code)
			}
		})
	}
}
