package pve

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/ssh"
)

// CommandRunner abstracts the host SSH transport. ssh.Runner satisfies it;
// tests inject fakes.
type CommandRunner interface {
	Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, command string, args ...string) (ssh.Result, error)
}

// RemoteExitError preserves a remote command's exit code as the process exit
// status. CLI semantic exit codes never overwrite it: a failing container
// command must be observable by automation exactly as it failed.
type RemoteExitError struct {
	Code int
}

func (e *RemoteExitError) Error() string {
	return fmt.Sprintf("remote command exited with code %d", e.Code)
}

func (e *RemoteExitError) ExitCode() int { return e.Code }

// LXCExecService executes commands inside LXC containers through the host
// SSH transport (`pct exec`). The REST API has no equivalent endpoint; this
// path is only taken for the optional SSH-enabled profiles.
type LXCExecService struct {
	guests *GuestService
	runner CommandRunner
}

func NewLXCExecService(guests *GuestService, runner CommandRunner) *LXCExecService {
	return &LXCExecService{guests: guests, runner: runner}
}

// LXCExecOptions controls one container command execution.
type LXCExecOptions struct {
	Node    string
	Command string
	Args    []string
	Timeout time.Duration
	// Stdin is forwarded to the remote command when non-nil.
	Stdin io.Reader
	// Stdout and Stderr are optional live passthrough sinks (text mode);
	// structured output always uses the bounded capture instead.
	Stdout io.Writer
	Stderr io.Writer
}

// Exec runs the command once. The returned AgentExecResult reuses the
// existing agent-exec result structure; errors distinguish remote exit codes
// (RemoteExitError) from SSH connection failures (*ssh.ConnectError).
func (s *LXCExecService) Exec(ctx context.Context, vmid int, options LXCExecOptions) (output.AgentExecResult, error) {
	if vmid <= 0 {
		return output.AgentExecResult{}, fmt.Errorf("%w: invalid vmid %d", ErrInvalid, vmid)
	}
	if options.Command == "" {
		return output.AgentExecResult{}, fmt.Errorf("%w: a command is required after --", ErrInvalid)
	}
	// Fail fast when the container does not exist or is ambiguous.
	if _, err := s.guests.resolve(ctx, vmid, options.Node); err != nil {
		return output.AgentExecResult{}, err
	}

	runCtx := ctx
	if options.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, options.Timeout)
		defer cancel()
	}

	arguments := make([]string, 0, len(options.Args)+4)
	arguments = append(arguments, "exec", fmt.Sprint(vmid), "--", options.Command)
	arguments = append(arguments, options.Args...)

	result, err := s.runner.Run(runCtx, options.Stdin, options.Stdout, options.Stderr, "pct", arguments...)
	execResult := output.AgentExecResult{
		ExitCode:  result.ExitCode,
		Stdout:    result.Stdout,
		Stderr:    result.Stderr,
		Truncated: result.StdoutTruncated || result.StderrTruncated,
	}
	if err != nil {
		return execResult, err
	}
	if result.ExitCode != 0 {
		return execResult, &RemoteExitError{Code: result.ExitCode}
	}
	return execResult, nil
}
