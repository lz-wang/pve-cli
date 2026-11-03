package pve

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/ssh"
)

type fakeCommandRunner struct {
	called  bool
	command string
	args    []string
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
	ctx     context.Context
	result  ssh.Result
	err     error
}

func (r *fakeCommandRunner) Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, command string, args ...string) (ssh.Result, error) {
	r.called = true
	r.ctx = ctx
	r.stdin = stdin
	r.stdout = stdout
	r.stderr = stderr
	r.command = command
	r.args = args
	return r.result, r.err
}

func newExecService(runner *fakeCommandRunner) (*GuestService, *LXCExecService) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		lxcs:  map[string]map[int]*fakeGuest{"pve1": {101: {row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve1"}}}},
	}
	guests := NewLXCService(backend, TaskRunner{}, nil, false)
	return guests, NewLXCExecService(guests, runner)
}

// The service translates one exec request into a single pct exec host
// command with the container ID and the user command separated by --.
func TestLXCExecRunsSinglePctExec(t *testing.T) {
	runner := &fakeCommandRunner{}
	guests, svc := newExecService(runner)

	result, err := svc.Exec(context.Background(), 101, LXCExecOptions{
		Node:    "pve1",
		Command: "/usr/bin/systemctl",
		Args:    []string{"status", "nginx"},
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if runner.command != "pct" || strings.Join(runner.args, " ") != "exec 101 -- /usr/bin/systemctl status nginx" {
		t.Fatalf("host command = %q %v", runner.command, runner.args)
	}
	if result.ExitCode != 0 {
		t.Fatalf("result = %+v", result)
	}
	_ = guests
}

// Command and arguments reach the transport as separate words; quoting is
// the transport's job, the service must not pre-join them.
func TestLXCExecKeepsArgumentsSeparate(t *testing.T) {
	runner := &fakeCommandRunner{}
	_, svc := newExecService(runner)

	_, err := svc.Exec(context.Background(), 101, LXCExecOptions{
		Command: "echo",
		Args:    []string{"two words", "a;b"},
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if len(runner.args) != 6 || runner.args[4] != "two words" || runner.args[5] != "a;b" {
		t.Fatalf("args = %v", runner.args)
	}
}

func TestLXCExecValidations(t *testing.T) {
	runner := &fakeCommandRunner{}
	_, svc := newExecService(runner)

	if _, err := svc.Exec(context.Background(), 0, LXCExecOptions{Command: "true"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if _, err := svc.Exec(context.Background(), -1, LXCExecOptions{Command: "true"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if _, err := svc.Exec(context.Background(), 101, LXCExecOptions{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if runner.called {
		t.Fatal("runner must not be called for invalid input")
	}
}

// Remote failures keep their exit code through RemoteExitError.
func TestLXCExecPreservesRemoteExitCode(t *testing.T) {
	runner := &fakeCommandRunner{result: ssh.Result{ExitCode: 7, Stdout: "partial"}}
	_, svc := newExecService(runner)

	result, err := svc.Exec(context.Background(), 101, LXCExecOptions{Command: "false"})
	var remote *RemoteExitError
	if !errors.As(err, &remote) || remote.Code != 7 {
		t.Fatalf("err = %v, want remote exit 7", err)
	}
	if result.ExitCode != 7 || result.Stdout != "partial" {
		t.Fatalf("result = %+v", result)
	}
}

// SSH connection failures stay ConnectError, never a remote exit code.
func TestLXCExecConnectFailureIsDistinct(t *testing.T) {
	runner := &fakeCommandRunner{err: ssh.NewConnectError(
		"ssh connection failed: ssh: connect to host pve1 port 22: Connection refused",
		"ssh: connect to host pve1 port 22: Connection refused")}
	_, svc := newExecService(runner)

	_, err := svc.Exec(context.Background(), 101, LXCExecOptions{Command: "true"})
	var connect *ssh.ConnectError
	if !errors.As(err, &connect) {
		t.Fatalf("err = %v (%T), want ConnectError", err, err)
	}
	var remote *RemoteExitError
	if errors.As(err, &remote) {
		t.Fatalf("connect failure must not surface as remote exit: %v", err)
	}
}

func TestLXCExecForwardsStreamsAndStdin(t *testing.T) {
	runner := &fakeCommandRunner{result: ssh.Result{ExitCode: 0, Stdout: "captured"}}
	_, svc := newExecService(runner)

	var live bytesBufferAlias
	var stdinReader io.Reader = strings.NewReader("payload")
	result, err := svc.Exec(context.Background(), 101, LXCExecOptions{Command: "cat", Stdin: stdinReader, Stdout: &live, Stderr: &live})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if runner.stdin == nil {
		t.Fatal("stdin not forwarded")
	}
	if runner.stdout == nil || runner.stderr == nil {
		t.Fatal("passthrough sinks not forwarded")
	}
	if result.Stdout != "captured" {
		t.Fatalf("captured stdout = %q", result.Stdout)
	}
}

func TestLXCExecAppliesTimeout(t *testing.T) {
	runner := &fakeCommandRunner{}
	_, svc := newExecService(runner)

	_, err := svc.Exec(context.Background(), 101, LXCExecOptions{Command: "true", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	deadline, ok := runner.ctx.Deadline()
	if !ok || deadline.IsZero() {
		t.Fatal("timeout context not applied")
	}
}

func TestLXCExecMapsTruncation(t *testing.T) {
	runner := &fakeCommandRunner{result: ssh.Result{ExitCode: 0, StdoutTruncated: true}}
	_, svc := newExecService(runner)

	result, err := svc.Exec(context.Background(), 101, LXCExecOptions{Command: "true"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !result.Truncated {
		t.Fatalf("truncation flag lost: %+v", result)
	}
}

type bytesBufferAlias struct{ data []byte }

func (b *bytesBufferAlias) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}
