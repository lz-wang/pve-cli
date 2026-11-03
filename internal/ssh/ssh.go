// Package ssh provides an optional host SSH transport that reuses the
// system OpenSSH client. The PVE API token keeps managing resources; SSH is
// only used for host operations the REST API cannot express (pct exec,
// offline mount, backup file transfer).
//
// Safety contract:
//   - Host identity is always verified against known_hosts; there is no
//     configuration to disable verification.
//   - Commands are executed with strictly POSIX-quoted arguments; no
//     unescaped shell string is ever assembled.
//   - Remote exit codes are data (Result.ExitCode); SSH connection and
//     protocol failures are separate ConnectError values.
//   - Output is captured bounded and never logged by default.
package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Output bounds: larger payloads are truncated and flagged.
const (
	stdoutLimit = 256 << 10
	stderrLimit = 64 << 10

	defaultPort         = 22
	defaultUser         = "root"
	defaultHostKeyCheck = "accept-new"
	connectTimeoutSecs  = 15
)

// Profile is the optional SSH section of a PVE profile. Credentials are
// independent from the API token: only key files and the system agent are
// used (BatchMode), so no secrets are stored here.
type Profile struct {
	Host         string `yaml:"host,omitempty"`
	User         string `yaml:"user,omitempty"`
	Port         int    `yaml:"port,omitempty"`
	IdentityFile string `yaml:"identity_file,omitempty"`
	KnownHosts   string `yaml:"known_hosts,omitempty"`

	// HostKeyChecking is fixed to accept-new in normalized(); the field
	// exists so Run can record the effective policy without ever accepting
	// "no".
	HostKeyChecking string `yaml:"-"`
}

// Validate enforces the profile invariants.
func (p Profile) Validate() error {
	if strings.TrimSpace(p.Host) == "" {
		return errors.New("ssh host is required")
	}
	if p.Port < 0 || p.Port > 65535 {
		return fmt.Errorf("ssh port %d out of range", p.Port)
	}
	return nil
}

func (p Profile) normalized() Profile {
	normalized := p
	if strings.TrimSpace(normalized.User) == "" {
		normalized.User = defaultUser
	}
	if normalized.Port == 0 {
		normalized.Port = defaultPort
	}
	if normalized.HostKeyChecking == "" {
		normalized.HostKeyChecking = defaultHostKeyCheck
	}
	if normalized.HostKeyChecking == "no" {
		normalized.HostKeyChecking = defaultHostKeyCheck
	}
	return normalized
}

// Result carries the bounded streams and exit status of one remote command.
type Result struct {
	ExitCode        int
	Stdout          string
	Stderr          string
	StdoutTruncated bool
	StderrTruncated bool
}

// ConnectError marks SSH connection, authentication or protocol failures as
// distinct from remote command exit codes.
type ConnectError struct {
	detail string
	stderr string
}

func (e *ConnectError) Error() string  { return e.detail }
func (e *ConnectError) Stderr() string { return e.stderr }

// Runner executes remote commands through the system ssh binary.
type Runner struct {
	profile Profile
	exec    func(ctx context.Context, argv []string, stdin io.Reader, stdout, stderr io.Writer) (int, error)
}

func NewRunner(profile Profile) *Runner {
	return &Runner{profile: profile}
}

// Run executes command with args on the profile host. The remote exit code is
// returned inside Result; a non-nil error means the command could not be
// executed or classified (launch failure, context cancellation, or an SSH
// connection-level failure wrapped in ConnectError).
func (r *Runner) Run(ctx context.Context, stdin io.Reader, command string, args ...string) (Result, error) {
	if err := r.profile.Validate(); err != nil {
		return Result{}, fmt.Errorf("ssh profile: %w", err)
	}
	profile := r.profile.normalized()
	execFn := r.exec
	if execFn == nil {
		execFn = execCommand
	}

	var stdout, stderr limitedBuffer
	stdout.limit = stdoutLimit
	stderr.limit = stderrLimit

	exit, err := execFn(ctx, r.argv(profile, command, args), stdin, &stdout, &stderr)
	result := Result{
		ExitCode:        exit,
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		StdoutTruncated: stdout.truncated,
		StderrTruncated: stderr.truncated,
	}
	if err != nil {
		if isConnectFailure(exit, stderr.String()) {
			return result, &ConnectError{detail: fmt.Sprintf("ssh connection failed: %s", strings.TrimSpace(stderr.String())), stderr: stderr.String()}
		}
		return result, err
	}
	if exit == 255 {
		if stderrText := stderr.String(); isConnectFailure(exit, stderrText) {
			return result, &ConnectError{detail: fmt.Sprintf("ssh connection failed: %s", strings.TrimSpace(stderrText)), stderr: stderrText}
		}
	}
	return result, nil
}

// argv assembles the ssh command line. The remote command travels as a
// single pre-quoted argv element.
func (r *Runner) argv(profile Profile, command string, args []string) []string {
	argv := []string{
		"ssh",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=" + profile.HostKeyChecking,
		"-o", "ConnectTimeout=" + strconv.Itoa(connectTimeoutSecs),
	}
	if strings.TrimSpace(profile.KnownHosts) != "" {
		argv = append(argv, "-o", "UserKnownHostsFile="+strings.TrimSpace(profile.KnownHosts))
	}
	if strings.TrimSpace(profile.IdentityFile) != "" {
		argv = append(argv, "-i", strings.TrimSpace(profile.IdentityFile))
	}
	if profile.Port != 0 && profile.Port != defaultPort {
		argv = append(argv, "-p", strconv.Itoa(profile.Port))
	}
	destination := profile.Host
	if strings.TrimSpace(profile.User) != "" {
		destination = profile.User + "@" + profile.Host
	}
	argv = append(argv, destination, remoteCommand(command, args...))
	return argv
}

// remoteCommand builds the single argument the remote shell will parse. Each
// word is POSIX single-quoted, so no metacharacter in command or args can
// alter the shell interpretation.
func remoteCommand(command string, args ...string) string {
	words := make([]string, 0, len(args)+1)
	words = append(words, quoteRemoteWord(command))
	for _, arg := range args {
		words = append(words, quoteRemoteWord(arg))
	}
	return strings.Join(words, " ")
}

func quoteRemoteWord(word string) string {
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// execCommand is the default executor over the system ssh binary.
func execCommand(ctx context.Context, argv []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if len(argv) == 0 {
		return -1, errors.New("empty command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	}
	return cmd.ProcessState.ExitCode(), nil
}

// connectFailurePatterns match ssh's own diagnostics. Only ssh's reserved
// exit status (255) combined with these patterns counts as a connection
// failure; a remote command legitimately exiting 255 stays a remote exit.
var connectFailurePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)connection refused`),
	regexp.MustCompile(`(?i)connection timed out|operation timed out|timed out`),
	regexp.MustCompile(`(?i)could not resolve hostname`),
	regexp.MustCompile(`(?i)no route to host`),
	regexp.MustCompile(`(?i)host key verification failed`),
	regexp.MustCompile(`(?i)permission denied`),
	regexp.MustCompile(`(?i)connection (closed|reset) by (remote host|peer)`),
	regexp.MustCompile(`(?i)kex_exchange_identification`),
	regexp.MustCompile(`(?i)usage:`),
	regexp.MustCompile(`(?i)no such identity`),
}

func isConnectFailure(exit int, stderrText string) bool {
	if exit != 255 {
		return false
	}
	for _, pattern := range connectFailurePatterns {
		if pattern.MatchString(stderrText) {
			return true
		}
	}
	return false
}

// limitedBuffer keeps at most limit bytes and records truncation.
type limitedBuffer struct {
	limit     int
	data      []byte
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		b.limit = stdoutLimit
	}
	room := b.limit - len(b.data)
	if room <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > room {
		b.data = append(b.data, p[:room]...)
		b.truncated = true
		return len(p), nil
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *limitedBuffer) String() string { return string(b.data) }
