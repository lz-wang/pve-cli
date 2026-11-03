package ssh

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type recordedExec struct {
	argv    []string
	stdin   string
	exit    int
	stdout  string
	stderr  string
	launch  error
	ctxDone bool
}

func (e *recordedExec) run(ctx context.Context, argv []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if err := ctx.Err(); err != nil {
		e.ctxDone = true
		return -1, err
	}
	e.argv = argv
	if stdin != nil {
		data, _ := io.ReadAll(stdin)
		e.stdin = string(data)
	}
	_, _ = stdout.Write([]byte(e.stdout))
	_, _ = stderr.Write([]byte(e.stderr))
	return e.exit, e.launch
}

func newTestRunner(exec *recordedExec, profile Profile) *Runner {
	runner := NewRunner(profile)
	runner.exec = exec.run
	return runner
}

func testProfile() Profile {
	return Profile{Host: "pve8845", User: "root", Port: 22, IdentityFile: "/home/op/.ssh/id_ed25519"}
}

func TestRunnerBuildsSafeArgv(t *testing.T) {
	exec := &recordedExec{}
	runner := newTestRunner(exec, testProfile())

	if _, err := runner.Run(context.Background(), nil, nil, nil, "pct", "exec", "101", "--", "systemctl", "status", "nginx"); err != nil {
		t.Fatalf("run: %v", err)
	}
	argv := exec.argv
	if argv[0] != "ssh" {
		t.Fatalf("argv[0] = %q", argv[0])
	}
	joined := strings.Join(argv, " ")
	for _, want := range []string{
		"-o BatchMode=yes",
		"-o StrictHostKeyChecking=accept-new",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("argv %v missing %q", argv, want)
		}
	}
	// Without an explicit known_hosts path the ssh defaults apply, which
	// include the system-wide file; no UserKnownHostsFile option is forced.
	for _, arg := range argv {
		if strings.HasPrefix(arg, "UserKnownHostsFile=") {
			t.Fatalf("unexpected forced known hosts: %v", argv)
		}
	}
	if strings.Contains(joined, "StrictHostKeyChecking=no") {
		t.Fatalf("host key verification must never be disabled: %v", argv)
	}
	if !containsPair(argv, "-i", "/home/op/.ssh/id_ed25519") {
		t.Fatalf("identity file missing: %v", argv)
	}
	if argv[len(argv)-2] != "root@pve8845" {
		t.Fatalf("destination = %v", argv)
	}
	remote := argv[len(argv)-1]
	if remote != "'pct' 'exec' '101' '--' 'systemctl' 'status' 'nginx'" {
		t.Fatalf("remote command = %q", remote)
	}
}

func containsPair(argv []string, flag, value string) bool {
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}

// Every argument is POSIX-quoted before being joined for the remote shell;
// no unescaped shell metacharacter ever reaches the wire.
func TestRunnerQuotesArguments(t *testing.T) {
	exec := &recordedExec{}
	runner := newTestRunner(exec, Profile{Host: "h"})

	_, err := runner.Run(context.Background(), nil, nil, nil, "/usr/bin/echo",
		"two words", "it's", "$(rm -rf /)", "a;b|c&&d", `back\slash`, "newline\ninjected")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	remote := exec.argv[len(exec.argv)-1]
	expected := `'/usr/bin/echo' 'two words' 'it'\''s' '$(rm -rf /)' 'a;b|c&&d' 'back\slash' 'newline
injected'`
	if remote != expected {
		t.Fatalf("remote = %q, want %q", remote, expected)
	}
}

func TestRunnerSeparatesExitCodeFromConnectFailure(t *testing.T) {
	cases := []struct {
		name        string
		exit        int
		stderr      string
		wantConnect bool
		wantCode    int
	}{
		{"remote exit 7", 7, "service failed", false, 7},
		{"remote exit 255 without ssh diagnostics", 255, "app failed hard", false, 255},
		{"connection refused", 255, "ssh: connect to host pve8845 port 22: Connection refused", true, 0},
		{"host key verification failed", 255, "Host key verification failed.", true, 0},
		{"auth failed", 255, "root@pve8845: Permission denied (publickey).", true, 0},
		{"dns failure", 255, "ssh: Could not resolve hostname pve8845: nodename nor servname provided", true, 0},
		{"timeout", 255, "ssh: connect to host pve8845 port 22: Operation timed out", true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exec := &recordedExec{exit: tc.exit, stdout: "out", stderr: tc.stderr}
			runner := newTestRunner(exec, Profile{Host: "pve8845"})
			result, err := runner.Run(context.Background(), nil, nil, nil, "true")
			if tc.wantConnect {
				var connect *ConnectError
				if !errors.As(err, &connect) {
					t.Fatalf("err = %v, want ConnectError", err)
				}
				if !strings.Contains(connect.Error(), tc.stderr) && connect.Stderr() != tc.stderr {
					t.Fatalf("connect error detail = %q", connect.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if result.ExitCode != tc.wantCode {
				t.Fatalf("exit = %d, want %d", result.ExitCode, tc.wantCode)
			}
			if result.Stdout != "out" || result.Stderr != tc.stderr {
				t.Fatalf("streams = %+v", result)
			}
		})
	}
}

func TestRunnerStreamsStdin(t *testing.T) {
	exec := &recordedExec{stdout: "ok"}
	runner := newTestRunner(exec, Profile{Host: "h"})

	result, err := runner.Run(context.Background(), strings.NewReader("payload"), nil, nil, "cat")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if exec.stdin != "payload" {
		t.Fatalf("stdin = %q", exec.stdin)
	}
	if result.Stdout != "ok" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
}

// Live passthrough sinks receive the full stream while capture stays
// bounded for classification and structured output.
func TestRunnerTeesToPassthroughSinks(t *testing.T) {
	exec := &recordedExec{stdout: strings.Repeat("a", 4<<20), stderr: "warn"}
	runner := newTestRunner(exec, Profile{Host: "h"})

	var liveOut, liveErr bytes.Buffer
	result, err := runner.Run(context.Background(), nil, &liveOut, &liveErr, "true")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if liveOut.Len() != 4<<20 {
		t.Fatalf("passthrough stdout len = %d, want full stream", liveOut.Len())
	}
	if liveErr.String() != "warn" {
		t.Fatalf("passthrough stderr = %q", liveErr.String())
	}
	if !result.StdoutTruncated || len(result.Stdout) != stdoutLimit {
		t.Fatalf("capture len = %d truncated = %v", len(result.Stdout), result.StdoutTruncated)
	}
	if result.Stderr != "warn" {
		t.Fatalf("captured stderr = %q", result.Stderr)
	}
}

func TestRunnerBoundsOutput(t *testing.T) {
	exec := &recordedExec{stdout: strings.Repeat("a", 4<<20), stderr: strings.Repeat("b", 4<<20)}
	runner := newTestRunner(exec, Profile{Host: "h"})

	result, err := runner.Run(context.Background(), nil, nil, nil, "true")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(result.Stdout) > stdoutLimit || !result.StdoutTruncated {
		t.Fatalf("stdout len = %d truncated = %v", len(result.Stdout), result.StdoutTruncated)
	}
	if len(result.Stderr) > stderrLimit || !result.StderrTruncated {
		t.Fatalf("stderr len = %d truncated = %v", len(result.Stderr), result.StderrTruncated)
	}
}

func TestRunnerRespectsContext(t *testing.T) {
	exec := &recordedExec{}
	runner := newTestRunner(exec, Profile{Host: "h"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := runner.Run(ctx, nil, nil, nil, "true"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want canceled", err)
	}
}

func TestRunnerLaunchFailure(t *testing.T) {
	exec := &recordedExec{launch: errors.New("binary not found")}
	runner := newTestRunner(exec, Profile{Host: "h"})

	if _, err := runner.Run(context.Background(), nil, nil, nil, "true"); err == nil {
		t.Fatal("expected launch error")
	}
}

func TestProfileValidation(t *testing.T) {
	if err := (Profile{}).Validate(); err == nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("err = %v, want missing host", err)
	}
	if err := (Profile{Host: "h", Port: 70000}).Validate(); err == nil {
		t.Fatal("expected port error")
	}
	if err := (Profile{Host: "h", Port: -1}).Validate(); err == nil {
		t.Fatal("expected port error")
	}
	if err := (Profile{Host: "h"}).Validate(); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestProfileDefaults(t *testing.T) {
	normalized := Profile{Host: "h"}.normalized()
	if normalized.User != "root" || normalized.Port != 22 {
		t.Fatalf("normalized = %+v", normalized)
	}
	if normalized.HostKeyChecking != "accept-new" {
		t.Fatalf("host key policy = %q", normalized.HostKeyChecking)
	}
}

func TestRunnerFailsWithoutHost(t *testing.T) {
	exec := &recordedExec{}
	runner := newTestRunner(exec, Profile{})
	if _, err := runner.Run(context.Background(), nil, nil, nil, "true"); err == nil {
		t.Fatal("expected profile error")
	}
}

func TestRunnerUsesCustomKnownHosts(t *testing.T) {
	exec := &recordedExec{}
	runner := newTestRunner(exec, Profile{Host: "h", KnownHosts: "/etc/pve/known_hosts"})
	if _, err := runner.Run(context.Background(), nil, nil, nil, "true"); err != nil {
		t.Fatal(err)
	}
	if !containsPair(exec.argv, "-o", "UserKnownHostsFile=/etc/pve/known_hosts") {
		t.Fatalf("known hosts missing: %v", exec.argv)
	}
}

func TestRunnerOmitsPortArgForDefault(t *testing.T) {
	exec := &recordedExec{}
	runner := newTestRunner(exec, Profile{Host: "h", Port: 2222})
	if _, err := runner.Run(context.Background(), nil, nil, nil, "true"); err != nil {
		t.Fatal(err)
	}
	if !containsPair(exec.argv, "-p", "2222") {
		t.Fatalf("port missing: %v", exec.argv)
	}
}

var _ = bytes.MinRead
