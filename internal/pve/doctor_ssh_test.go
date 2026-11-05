package pve

import (
	"context"
	"errors"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/ssh"
	"io"
	"strings"
	"testing"
)

type doctorSSHRunner struct {
	host    string
	missing string
	calls   int
	fail    bool
}

func (r *doctorSSHRunner) Run(ctx context.Context, _ io.Reader, _, _ io.Writer, command string, args ...string) (ssh.Result, error) {
	r.calls++
	if r.fail {
		return ssh.Result{Stderr: "PRIVATE-REMOTE"}, errors.New("PRIVATE-REMOTE")
	}
	if ctx.Err() != nil {
		return ssh.Result{}, ctx.Err()
	}
	switch command {
	case "hostname":
		return ssh.Result{Stdout: r.host + "\n"}, nil
	case "id":
		return ssh.Result{Stdout: "0\n"}, nil
	case "sh":
		if len(args) != 4 || args[0] != "-c" || args[1] != `command -v "$1" >/dev/null 2>&1` {
			return ssh.Result{}, errors.New("unexpected shell probe")
		}
		if args[3] == r.missing {
			return ssh.Result{ExitCode: 1}, nil
		}
		return ssh.Result{}, nil
	default:
		return ssh.Result{}, errors.New("unexpected command")
	}
}
func TestDoctorSSHChecks(t *testing.T) {
	for _, tc := range []struct {
		name       string
		profile    *ssh.Profile
		offline    bool
		known      bool
		host       string
		missing    string
		fail       bool
		wantFailed bool
		wantCalls  int
	}{
		{name: "healthy", profile: &ssh.Profile{Host: "lab", Node: "n"}, known: true, host: "n", wantCalls: 13},
		{name: "offline", profile: &ssh.Profile{Host: "lab", Node: "n"}, offline: true},
		{name: "no ssh", wantFailed: true},
		{name: "no node", profile: &ssh.Profile{Host: "lab"}, known: true, wantFailed: true},
		{name: "api unknown", profile: &ssh.Profile{Host: "lab", Node: "n"}, wantFailed: true},
		{name: "wrong host", profile: &ssh.Profile{Host: "lab", Node: "n"}, known: true, host: "other", wantFailed: true, wantCalls: 1},
		{name: "missing tool", profile: &ssh.Profile{Host: "lab", Node: "n"}, known: true, host: "n", missing: "pct", wantFailed: true, wantCalls: 13},
		{name: "connection error", profile: &ssh.Profile{Host: "lab", Node: "n"}, known: true, fail: true, wantFailed: true, wantCalls: 1},
		{name: "missing key", profile: &ssh.Profile{Host: "lab", Node: "n", IdentityFile: "/missing/key"}, known: true, wantFailed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := DoctorResult{}
			runner := &doctorSSHRunner{host: tc.host, missing: tc.missing, fail: tc.fail}
			r.checkSSH(context.Background(), tc.profile, DoctorOptions{SSH: true, Offline: tc.offline}, []output.NodeRow{{Name: "n"}}, tc.known, func(p ssh.Profile) CommandRunner {
				if p.HostKeyChecking != "yes" {
					t.Error("doctor must use strict host keys")
				}
				return runner
			})
			if r.Failed != tc.wantFailed || runner.calls != tc.wantCalls {
				t.Fatalf("failed=%v calls=%d rows=%+v", r.Failed, runner.calls, r.Rows)
			}
			for _, row := range r.Rows {
				if strings.Contains(row.Message, "PRIVATE") {
					t.Fatal("raw remote output leaked")
				}
			}
		})
	}
}
