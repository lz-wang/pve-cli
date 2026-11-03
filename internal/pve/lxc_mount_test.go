package pve

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/ssh"
)

func mountHarness(t *testing.T, status string, script map[string]ssh.Result) (*MountService, *scriptedRunner, *fakeGuest) {
	t.Helper()
	guest := &fakeGuest{row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve8845", Status: status}}
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve8845", Status: "online"}},
		lxcs:  map[string]map[int]*fakeGuest{"pve8845": {101: guest}},
	}
	runner := newScriptedRunner()
	for command, result := range script {
		runner = runner.on(command, result)
	}
	guests := NewLXCService(backend, TaskRunner{}, nil, false)
	return NewMountService(guests, runner), runner, guest
}

// mountSequenceHarness scripts the mountpoint probe per call: first the
// pre-check result, then the post-verification result.
func mountSequenceHarness(t *testing.T, status string, preMounted bool, postMounted bool, pctResult ssh.Result) (*MountService, *scriptedRunner) {
	t.Helper()
	guest := &fakeGuest{row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve8845", Status: status}}
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve8845", Status: "online"}},
		lxcs:  map[string]map[int]*fakeGuest{"pve8845": {101: guest}},
	}
	pre := ssh.Result{ExitCode: 1}
	if preMounted {
		pre = ssh.Result{ExitCode: 0}
	}
	post := ssh.Result{ExitCode: 1}
	if postMounted {
		post = ssh.Result{ExitCode: 0}
	}
	runner := newScriptedRunner().
		onSeq("mountpoint -q "+mountRootfs, pre, post).
		on("pct mount 101", pctResult)
	return NewMountService(NewLXCService(backend, TaskRunner{}, nil, false), runner), runner
}

const mountRootfs = "/var/lib/lxc/101/rootfs"

// Happy path: stopped container, not yet mounted, pct mount succeeds and
// the result reports the real host path plus confirmation.
func TestLXCMountReportsHostPath(t *testing.T) {
	service, runner := mountSequenceHarness(t, "stopped", false, true,
		ssh.Result{ExitCode: 0, Stdout: "filesystem is mounted successfully at " + mountRootfs + "\n"})

	result, err := service.Mount(context.Background(), 101, "pve8845")
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	if result.Status != "succeeded" || !result.Mounted || result.Path != mountRootfs {
		t.Fatalf("result = %+v", result)
	}
	if !runner.saw("pct mount 101") {
		t.Fatal("pct mount not invoked")
	}
}

// The mount state is re-verified after pct mount; an unverifiable state is
// reported as unknown with the manual-cleanup hint, never as success.
func TestLXCMountUnverifiableIsUnknown(t *testing.T) {
	service, _ := mountSequenceHarness(t, "stopped", false, false, ssh.Result{ExitCode: 0, Stdout: "ok"})

	result, err := service.Mount(context.Background(), 101, "pve8845")
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	if result.Status != "unknown" || result.Mounted {
		t.Fatalf("result = %+v, want unknown/unmounted", result)
	}
	if !strings.Contains(result.Message, "manual") {
		t.Fatalf("cleanup hint missing: %q", result.Message)
	}
}

func TestLXCMountRequiresStoppedGuest(t *testing.T) {
	service, runner, _ := mountHarness(t, "running", nil)

	_, err := service.Mount(context.Background(), 101, "pve8845")
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "stop") {
		t.Fatalf("err = %v", err)
	}
	if runner.saw("pct mount") {
		t.Fatal("no host command on a running guest")
	}
}

func TestLXCMountRefusesDoubleMount(t *testing.T) {
	service, runner, _ := mountHarness(t, "stopped", map[string]ssh.Result{
		"mountpoint -q " + mountRootfs: {ExitCode: 0}, // already mounted
	})

	_, err := service.Mount(context.Background(), 101, "pve8845")
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "already mounted") {
		t.Fatalf("err = %v", err)
	}
	if runner.saw("pct mount 101") {
		t.Fatal("double mount must not re-run pct mount")
	}
}

func TestLXCMountRequiresExplicitNode(t *testing.T) {
	service, _, _ := mountHarness(t, "stopped", nil)

	if _, err := service.Mount(context.Background(), 101, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if _, err := service.Unmount(context.Background(), 101, " "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestLXCMountFailureHintsManualCleanup(t *testing.T) {
	service, _, _ := mountHarness(t, "stopped", map[string]ssh.Result{
		"mountpoint -q " + mountRootfs: {ExitCode: 1},
		"pct mount 101":                {ExitCode: 255, Stderr: "mount error"},
	})

	_, err := service.Mount(context.Background(), 101, "pve8845")
	if err == nil || !strings.Contains(err.Error(), "manual cleanup") {
		t.Fatalf("err = %v, want manual cleanup hint", err)
	}
}

func TestLXCMountFailureIncludesStderr(t *testing.T) {
	service, _, _ := mountHarness(t, "stopped", map[string]ssh.Result{
		"mountpoint -q " + mountRootfs: {ExitCode: 1},
		"pct mount 101":                {ExitCode: 1, Stderr: "dir is busy"},
	})

	_, err := service.Mount(context.Background(), 101, "pve8845")
	if err == nil || !strings.Contains(err.Error(), "dir is busy") {
		t.Fatalf("err = %v", err)
	}
}

// Unmount is idempotent: an unmounted container is reported, not acted on.
func TestLXCUnmountIdempotent(t *testing.T) {
	service, runner, _ := mountHarness(t, "stopped", map[string]ssh.Result{
		"mountpoint -q " + mountRootfs: {ExitCode: 1},
	})

	result, err := service.Unmount(context.Background(), 101, "pve8845")
	if err != nil {
		t.Fatalf("unmount: %v", err)
	}
	if result.Mounted || result.Status != "succeeded" {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Message, "not mounted") {
		t.Fatalf("message = %q", result.Message)
	}
	if runner.saw("pct unmount") {
		t.Fatal("no pct unmount needed")
	}
}

func TestLXCUnmountVerifiesState(t *testing.T) {
	service, runner, _ := mountHarness(t, "stopped", map[string]ssh.Result{
		"pct unmount 101": {ExitCode: 0, Stdout: "umounted"},
	})
	// First mountpoint check (mounted) returns the default exit 0, the
	// post-unmount verification also returns 0 -> still mounted -> unknown.
	result, err := service.Unmount(context.Background(), 101, "pve8845")
	if err != nil {
		t.Fatalf("unmount: %v", err)
	}
	if result.Status != "unknown" || !result.Mounted {
		t.Fatalf("result = %+v, want unknown/still-mounted", result)
	}
	if !runner.saw("pct unmount 101") {
		t.Fatal("pct unmount not invoked")
	}
}
