package pve

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/ssh"
)

// lifecycleHost simulates the host side of offline maintenance: the
// mountpoint, the files visible through it, and the container lock pct
// holds while mounted. It exists to validate the full mount → read →
// unmount lifecycle, lock observation and interrupted-run recovery without
// touching a real container.
type lifecycleHost struct {
	mounted      bool
	failMount    bool
	failUnmount  bool
	leaveMounted bool // unmount claims success but stays mounted
	guest        *fakeGuest
	configReads  int
}

type lifecycleBackend struct {
	*fakeBackend
	host *lifecycleHost
}

func (b *lifecycleBackend) GuestConfig(context.Context, string, string, int) (map[string]any, error) {
	b.host.configReads++
	config := map[string]any{"rootfs": "local-lvm:vm-101-disk-0,size=8G"}
	if b.host.mounted {
		config["lock"] = "mounted"
	}
	return config, nil
}

// Run implements CommandRunner over the simulated host.
func (h *lifecycleHost) Run(_ context.Context, stdin io.Reader, stdout, _ io.Writer, command string, args ...string) (ssh.Result, error) {
	if stdin != nil {
		done := make(chan struct{})
		go func() { _, _ = io.Copy(io.Discard, stdin); close(done) }()
		<-done
	}
	key := strings.Join(append([]string{command}, args...), " ")
	switch {
	case key == "mountpoint -q "+mountRootfs:
		if h.mounted {
			return ssh.Result{ExitCode: 0}, nil
		}
		return ssh.Result{ExitCode: 1}, nil
	case key == "pct mount 101":
		if h.failMount {
			return ssh.Result{ExitCode: 1, Stderr: "mount interrupted"}, nil
		}
		h.mounted = true
		return ssh.Result{ExitCode: 0, Stdout: "filesystem is mounted successfully at " + mountRootfs}, nil
	case key == "pct unmount 101":
		if h.failUnmount {
			return ssh.Result{ExitCode: 1, Stderr: "umount interrupted"}, nil
		}
		if h.leaveMounted {
			h.leaveMounted = false // one flaky attempt only
			return ssh.Result{ExitCode: 0, Stdout: "ok"}, nil
		}
		h.mounted = false
		return ssh.Result{ExitCode: 0, Stdout: "ok"}, nil
	case key == "cat "+mountRootfs+"/etc/hostname":
		if !h.mounted {
			return ssh.Result{ExitCode: 1, Stderr: "No such file"}, nil
		}
		return ssh.Result{ExitCode: 0, Stdout: "app-101\n"}, nil
	default:
		return ssh.Result{ExitCode: 0}, nil
	}
}

func lifecycleService(host *lifecycleHost) (*MountService, *lifecycleBackend) {
	host.guest = &fakeGuest{row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve8845", Status: "stopped"}}
	backend := &lifecycleBackend{fakeBackend: &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve8845", Status: "online"}},
		lxcs:  map[string]map[int]*fakeGuest{"pve8845": {101: host.guest}},
	}, host: host}
	guests := NewLXCService(backend, TaskRunner{}, nil, false)
	return NewMountService(guests, host, "pve8845"), backend
}

// The full lifecycle: mount, read a file through the mount, observe the
// lock, unmount, observe the lock released and the mount gone.
func TestOfflineMaintenanceLifecycle(t *testing.T) {
	host := &lifecycleHost{}
	service, backend := lifecycleService(host)
	ctx := context.Background()

	mount, err := service.Mount(ctx, 101, "pve8845")
	if err != nil || mount.Status != "succeeded" || !mount.Mounted || mount.Path != mountRootfs {
		t.Fatalf("mount = %+v err = %v", mount, err)
	}

	// Reading through the mount works (host file access).
	read, err := host.Run(ctx, nil, nil, nil, "cat", mountRootfs+"/etc/hostname")
	if err != nil || read.ExitCode != 0 || strings.TrimSpace(read.Stdout) != "app-101" {
		t.Fatalf("read through mount = %+v err = %v", read, err)
	}

	// While mounted, the container lock is observable in its config.
	config, err := backend.GuestConfig(ctx, "lxc", "pve8845", 101)
	if err != nil {
		t.Fatal(err)
	}
	if config["lock"] != "mounted" {
		t.Fatalf("lock = %v, want lock held while mounted", config["lock"])
	}

	unmount, err := service.Unmount(ctx, 101, "pve8845")
	if err != nil || unmount.Status != "succeeded" || unmount.Mounted {
		t.Fatalf("unmount = %+v err = %v", unmount, err)
	}

	config, err = backend.GuestConfig(ctx, "lxc", "pve8845", 101)
	if err != nil {
		t.Fatal(err)
	}
	if _, locked := config["lock"]; locked {
		t.Fatalf("lock = %v, want released after unmount", config["lock"])
	}
	if host.mounted {
		t.Fatal("host state must be unmounted")
	}
}

// An interrupted mount leaves an ambiguous state: the CLI reports the
// failure with the cleanup hint, and recovery through pct unmount returns
// the host to a clean, remountable state.
func TestOfflineMaintenanceInterruptedMountRecovery(t *testing.T) {
	host := &lifecycleHost{failMount: true}
	service, _ := lifecycleService(host)
	ctx := context.Background()

	_, err := service.Mount(ctx, 101, "pve8845")
	if err == nil || !strings.Contains(err.Error(), "manual cleanup") {
		t.Fatalf("err = %v, want cleanup hint", err)
	}

	// Simulate the operator-side recovery: the host actually did mount
	// partially, so pct unmount is the documented repair.
	host.mounted = true
	host.failMount = false
	recovered, err := service.Unmount(ctx, 101, "pve8845")
	if err != nil || recovered.Status != "succeeded" || recovered.Mounted {
		t.Fatalf("recovery unmount = %+v err = %v", recovered, err)
	}

	// After recovery a fresh mount works again.
	remount, err := service.Mount(ctx, 101, "pve8845")
	if err != nil || remount.Status != "succeeded" || !remount.Mounted {
		t.Fatalf("remount = %+v err = %v", remount, err)
	}
}

// An unmount that claims success while the mount persists reports unknown;
// a retry completes the release.
func TestOfflineMaintenanceInterruptedUnmountRetry(t *testing.T) {
	host := &lifecycleHost{leaveMounted: true}
	service, _ := lifecycleService(host)
	ctx := context.Background()

	if _, err := service.Mount(ctx, 101, "pve8845"); err != nil {
		t.Fatalf("mount: %v", err)
	}

	first, err := service.Unmount(ctx, 101, "pve8845")
	if !errors.Is(err, ErrUncertain) {
		t.Fatalf("unmount err = %v, want ErrUncertain on the unconfirmed first attempt", err)
	}
	if first.Status != "unknown" || !first.Mounted {
		t.Fatalf("first unmount = %+v, want unknown/still-mounted", first)
	}

	second, err := service.Unmount(ctx, 101, "pve8845")
	if err != nil || second.Status != "succeeded" || second.Mounted {
		t.Fatalf("retry unmount = %+v err = %v", second, err)
	}
}

// A double mount attempt after a successful mount is refused and leaves
// the mounted state untouched.
func TestOfflineMaintenanceDoubleMountRefused(t *testing.T) {
	host := &lifecycleHost{}
	service, _ := lifecycleService(host)
	ctx := context.Background()

	if _, err := service.Mount(ctx, 101, "pve8845"); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if _, err := service.Mount(ctx, 101, "pve8845"); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want conflict", err)
	}
	if !host.mounted {
		t.Fatal("mounted state must be untouched by the refused attempt")
	}
}
