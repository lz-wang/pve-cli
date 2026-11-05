package pve

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/ssh"
)

func (s *DoctorService) WithSSH(factory func(ssh.Profile) CommandRunner) *DoctorService {
	s.sshRunnerFactory = factory
	return s
}

// checkSSH never prints remote output or credentials. Strict host-key checking
// prevents a diagnostic from modifying known_hosts on first contact.
func (r *DoctorResult) checkSSH(ctx context.Context, profile *ssh.Profile, options DoctorOptions, nodes []output.NodeRow, nodesKnown bool, factory func(ssh.Profile) CommandRunner) {
	if !options.SSH {
		return
	}
	if options.Offline {
		r.add("SSH", output.DoctorStatusSkip, "offline mode does not connect through SSH")
		return
	}
	if profile == nil {
		r.add("SSH_CONFIG", output.DoctorStatusFail, "profile has no SSH section; use config ssh set")
		return
	}
	if err := profile.Validate(); err != nil {
		r.add("SSH_CONFIG", output.DoctorStatusFail, "invalid SSH host or port")
		return
	}
	r.add("SSH_CONFIG", output.DoctorStatusOK, "SSH profile configured")
	filesOK := true
	for _, entry := range []struct{ name, path string }{{"SSH_IDENTITY_FILE", profile.IdentityFile}, {"SSH_KNOWN_HOSTS", profile.KnownHosts}} {
		if entry.path == "" {
			r.add(entry.name, output.DoctorStatusSkip, "uses OpenSSH defaults or agent")
			continue
		}
		path, err := config.ExpandPath(entry.path)
		var info os.FileInfo
		if err == nil {
			info, err = os.Stat(path)
		}
		if err != nil || !info.Mode().IsRegular() {
			filesOK = false
			r.add(entry.name, output.DoctorStatusFail, "configured path is not an accessible regular file")
		} else {
			r.add(entry.name, output.DoctorStatusOK, "configured file exists; contents were not read")
		}
	}
	bound := false
	for _, n := range nodes {
		if n.Name == profile.Node {
			bound = true
		}
	}
	if profile.Node == "" || (options.Node != "" && profile.Node != options.Node) {
		r.add("SSH_NODE", output.DoctorStatusFail, "SSH node binding is missing or differs from --node")
		return
	}
	if !nodesKnown {
		r.add("SSH_NODE", output.DoctorStatusUnknown, "API node inventory unavailable; SSH probes skipped")
		r.Failed = true
		return
	}
	if !bound {
		r.add("SSH_NODE", output.DoctorStatusFail, "SSH node binding is absent from API inventory")
		return
	}
	if !filesOK {
		r.add("SSH_CONNECTIVITY", output.DoctorStatusSkip, "local SSH file checks failed")
		return
	}
	if factory == nil {
		factory = func(p ssh.Profile) CommandRunner { return ssh.NewRunner(p) }
	}
	strict := *profile
	strict.HostKeyChecking = "yes"
	runner := factory(strict)
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	run, err := runner.Run(ctx, nil, nil, nil, "hostname", "-s")
	if err != nil || run.ExitCode != 0 || run.StdoutTruncated {
		r.add("SSH_CONNECTIVITY", output.DoctorStatusFail, "SSH hostname probe failed; verify authentication and known_hosts")
		return
	}
	r.add("SSH_CONNECTIVITY", output.DoctorStatusOK, "SSH connection succeeded with strict host-key checking")
	if strings.TrimSpace(run.Stdout) != profile.Node {
		r.add("SSH_NODE", output.DoctorStatusFail, "remote hostname differs from the declared PVE node; capability probes skipped")
		return
	}
	r.add("SSH_NODE", output.DoctorStatusOK, "remote hostname matches the declared API node")
	run, err = runner.Run(ctx, nil, nil, nil, "id", "-u")
	if err != nil || run.ExitCode != 0 || run.StdoutTruncated {
		r.add("SSH_USER", output.DoctorStatusFail, "cannot determine remote user ID")
	} else if strings.TrimSpace(run.Stdout) != "0" {
		r.add("SSH_USER", output.DoctorStatusWarn, "remote user is not root; host operations may lack permissions")
	} else {
		r.add("SSH_USER", output.DoctorStatusOK, "remote user is root")
	}
	for _, tool := range []string{"pvesm", "sha256sum", "dd", "cat", "stat", "mkdir", "ln", "rm", "test", "pct", "mountpoint"} {
		run, err = runner.Run(ctx, nil, nil, nil, "sh", "-c", `command -v "$1" >/dev/null 2>&1`, "pve-doctor", tool)
		status := output.DoctorStatusOK
		message := "command available"
		if err != nil || run.ExitCode != 0 {
			status = output.DoctorStatusFail
			message = "command unavailable or probe failed"
		}
		r.add("SSH_TOOL_"+tool, status, message)
	}
	r.add("SSH_CAPABILITIES", output.DoctorStatusWarn, "tool presence does not prove storage permissions, free space or successful container execution; no operation was performed")
}
