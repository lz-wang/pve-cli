package pve

import (
	"context"
	"fmt"
	"io"
	"strings"
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
// path is only taken for the optional SSH-enabled profiles. sshNode is the
// PVE node the SSH host runs (ssh.node in the profile); pct commands are
// refused unless the API-resolved guest node matches it.
type LXCExecService struct {
	guests  *GuestService
	runner  CommandRunner
	sshNode string
}

func NewLXCExecService(guests *GuestService, runner CommandRunner, sshNode string) *LXCExecService {
	return &LXCExecService{guests: guests, runner: runner, sshNode: sshNode}
}

// requireSSHNodeBinding enforces the first-version SSH contract: one SSH
// profile host maps to exactly one PVE node, declared as ssh.node in the
// profile. pct commands never run against a host whose node declaration is
// missing or disagrees with the API-resolved guest node — both hosts may
// host the same VMID.
func requireSSHNodeBinding(sshNode, guestNode string, vmid int, operation string) error {
	if strings.TrimSpace(sshNode) == "" {
		return fmt.Errorf("%w: the profile ssh section must declare node: <PVE node> before %s can run", ErrInvalid, operation)
	}
	if sshNode != guestNode {
		return fmt.Errorf("%w: the ssh host binds PVE node %q but container %d is on node %q; refusing to run %s there", ErrInvalid, sshNode, vmid, guestNode, operation)
	}
	return nil
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
	guest, err := s.guests.resolve(ctx, vmid, options.Node)
	if err != nil {
		return output.AgentExecResult{}, err
	}
	if err := requireSSHNodeBinding(s.sshNode, guest.Row().Node, vmid, "pct exec"); err != nil {
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
