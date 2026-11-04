package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/lz-wang/pve-cli/v3/internal/ssh"
)

type mountCmdRunner struct {
	calls     []string
	sequenced map[string][]ssh.Result
	defaults  ssh.Result
}

func (r *mountCmdRunner) Run(_ context.Context, stdin io.Reader, stdout io.Writer, _ io.Writer, command string, args ...string) (ssh.Result, error) {
	key := strings.Join(append([]string{command}, args...), " ")
	r.calls = append(r.calls, key)
	if stdin != nil {
		done := make(chan struct{})
		go func() { _, _ = io.Copy(io.Discard, stdin); close(done) }()
		<-done
	}
	result := r.defaults
	if queued := r.sequenced[key]; len(queued) > 0 {
		result = queued[0]
		r.sequenced[key] = queued[1:]
	}
	if stdout != nil && result.Stdout != "" {
		if _, err := stdout.Write([]byte(result.Stdout)); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (r *mountCmdRunner) saw(part string) bool {
	for _, call := range r.calls {
		if strings.Contains(call, part) {
			return true
		}
	}
	return false
}

func mountCmdDeps(runner *mountCmdRunner, guestRow output.GuestRow, stdout, stderr *bytes.Buffer) Dependencies {
	backend := &commandBackend{
		nodes:     []output.NodeRow{{Name: "pve8845", Status: "online"}},
		lxcs:      map[string][]output.GuestRow{"pve8845": {guestRow}},
		lxcGuests: map[string]map[int]*commandGuest{"pve8845": {101: {row: guestRow}}},
	}
	deps := commandDeps(stdout, stderr, backend)
	deps.SSHRunnerFactory = func(profile ssh.Profile) pve.CommandRunner { return runner }
	return deps
}

func TestLXCMountCommandEndToEnd(t *testing.T) {
	row := output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve8845", Status: "stopped"}
	runner := &mountCmdRunner{sequenced: map[string][]ssh.Result{
		"mountpoint -q /var/lib/lxc/101/rootfs": {
			{ExitCode: 1}, // pre: not mounted
			{ExitCode: 0}, // post: mounted
		},
	}, defaults: ssh.Result{ExitCode: 0, Stdout: "filesystem is mounted successfully at /var/lib/lxc/101/rootfs\n"}}
	var stdout, stderr bytes.Buffer
	deps := mountCmdDeps(runner, row, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeSSHTestConfig(t, "pve8845"),
		"lxc", "mount", "101", "--node", "pve8845", "-o", "json",
	}, "test", deps)
	if err != nil {
		t.Fatalf("mount: %v (stderr %s)", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{`"mounted": true`, `"/var/lib/lxc/101/rootfs"`, `"status": "succeeded"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}

func TestLXCMountCommandRefusesRunningGuest(t *testing.T) {
	row := output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve8845", Status: "running"}
	runner := &mountCmdRunner{}
	var stdout, stderr bytes.Buffer
	deps := mountCmdDeps(runner, row, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeSSHTestConfig(t, "pve8845"),
		"lxc", "mount", "101", "--node", "pve8845",
	}, "test", deps)
	if !errors.Is(err, pve.ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	if code := ExitCode(err); code != 4 {
		t.Fatalf("exit = %d, want 4", code)
	}
	if runner.saw("pct mount") {
		t.Fatal("no host command on a running guest")
	}
}

// P1-2: mount/unmount refuse an SSH host bound to a different PVE node.
func TestLXCMountCommandRejectsSSHNodeMismatch(t *testing.T) {
	row := output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve8845", Status: "stopped"}
	runner := &mountCmdRunner{}
	var stdout, stderr bytes.Buffer
	deps := mountCmdDeps(runner, row, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeSSHTestConfig(t, "n150"),
		"lxc", "mount", "101", "--node", "pve8845", "-o", "json",
	}, "test", deps)
	if err == nil || !strings.Contains(err.Error(), "n150") || !strings.Contains(err.Error(), "pve8845") {
		t.Fatalf("err = %v, want a node mismatch refusal", err)
	}
	if runner.saw("pct") {
		t.Fatal("pct mount must not run on the wrong SSH host")
	}

	err = RunWithDependencies([]string{
		"pve", "--config", writeSSHTestConfig(t, "n150"),
		"lxc", "unmount", "101", "--node", "pve8845", "-o", "json",
	}, "test", deps)
	if err == nil || !strings.Contains(err.Error(), "n150") {
		t.Fatalf("unmount err = %v, want a node mismatch refusal", err)
	}
	if runner.saw("pct") {
		t.Fatal("pct unmount must not run on the wrong SSH host")
	}
}

func TestLXCMountCommandRequiresNodeAndSSH(t *testing.T) {
	row := output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve8845", Status: "stopped"}
	runner := &mountCmdRunner{}
	var stdout, stderr bytes.Buffer

	err := RunWithDependencies([]string{
		"pve", "--config", writeSSHTestConfig(t, "pve8845"),
		"lxc", "mount", "101",
	}, "test", mountCmdDeps(runner, row, &stdout, &stderr))
	if err == nil || !strings.Contains(err.Error(), "--node") {
		t.Fatalf("err = %v", err)
	}

	err = RunWithDependencies([]string{
		"pve", "--config", writeTestConfig(t, "json"),
		"lxc", "mount", "101", "--node", "pve8845",
	}, "test", Dependencies{Stdout: &stdout, Stderr: &stderr, BackendFactory: mountCmdDeps(runner, row, &stdout, &stderr).BackendFactory})
	if err == nil || !strings.Contains(err.Error(), "ssh section") {
		t.Fatalf("err = %v", err)
	}
}

func TestLXCUnmountCommandEndToEnd(t *testing.T) {
	row := output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve8845", Status: "stopped"}
	runner := &mountCmdRunner{sequenced: map[string][]ssh.Result{
		"mountpoint -q /var/lib/lxc/101/rootfs": {
			{ExitCode: 0}, // pre: mounted
			{ExitCode: 1}, // post: released
		},
	}}
	var stdout, stderr bytes.Buffer
	deps := mountCmdDeps(runner, row, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeSSHTestConfig(t, "pve8845"),
		"lxc", "unmount", "101", "--node", "pve8845", "-o", "json",
	}, "test", deps)
	if err != nil {
		t.Fatalf("unmount: %v (stderr %s)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"mounted": false`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if !runner.saw("pct unmount 101") {
		t.Fatal("pct unmount not invoked")
	}
}
