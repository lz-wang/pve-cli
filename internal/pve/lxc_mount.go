package pve

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

// mountPathPattern extracts the host path pct mount reports.
var mountPathPattern = regexp.MustCompile(`(/var/lib/lxc/\d+/rootfs)`)

// MountService wraps `pct mount`/`pct unmount` over the host SSH transport.
// These are emergency-maintenance operations: pct holds container locks
// while mounted, so every step verifies observable state and refuses
// ambiguous situations instead of forcing them. There is no --force.
type MountService struct {
	guests  *GuestService
	runner  CommandRunner
	sshNode string
}

// NewMountService takes the PVE node the SSH host runs (ssh.node in the
// profile); pct mount/unmount are refused unless the resolved guest node
// matches it.
func NewMountService(guests *GuestService, runner CommandRunner, sshNode string) *MountService {
	return &MountService{guests: guests, runner: runner, sshNode: sshNode}
}

// Mount mounts a stopped container's rootfs on the host and returns the
// real host path. Refuses running guests and double mounts; an
// unverifiable outcome reports unknown with the manual-cleanup hint.
func (s *MountService) Mount(ctx context.Context, vmid int, node string) (output.LXCMountResult, error) {
	result := output.LXCMountResult{Resource: "lxc", VMID: vmid}
	node = strings.TrimSpace(node)
	if node == "" {
		return result, fmt.Errorf("%w: --node is required for mount/unmount", ErrInvalid)
	}
	result.Node = node
	guest, err := s.resolveStopped(ctx, vmid, node, "pct mount")
	if err != nil {
		return result, err
	}
	result.Node = guest.Row().Node
	path := lxcRootfsPath(vmid)
	result.Path = path

	mounted, err := s.pathMounted(ctx, path)
	if err != nil {
		return result, err
	}
	if mounted {
		result.Mounted = true
		result.Status = "failed"
		result.Message = fmt.Sprintf("%s is already mounted; refusing a double mount — unmount first", path)
		return result, fmt.Errorf("%w: %s", ErrConflict, result.Message)
	}

	run, err := s.runner.Run(ctx, nil, nil, nil, "pct", "mount", strconv.Itoa(vmid))
	if err != nil {
		return result, err
	}
	if run.ExitCode != 0 {
		result.Status = "failed"
		result.Message = fmt.Sprintf("pct mount exited with %d; manual cleanup may be required (check locks, run pct unmount)", run.ExitCode)
		return result, fmt.Errorf("%w: pct mount %d failed: %s; manual cleanup may be required", ErrConflict, vmid, strings.TrimSpace(run.Stderr))
	}
	if reported := mountPathPattern.FindString(run.Stdout); reported != "" {
		result.Path = reported
		path = reported
	}

	confirmed, err := s.pathMounted(ctx, path)
	if err != nil {
		return result, err
	}
	result.Mounted = confirmed
	if !confirmed {
		result.Status = "unknown"
		result.Message = "pct mount returned success but the mount could not be confirmed; manual cleanup may be required (check locks, run pct unmount)"
		return result, nil
	}
	result.Status = "succeeded"
	result.Message = "rootfs mounted on the host; container locks are held until unmount"
	return result, nil
}

// Unmount releases a mounted rootfs. Idempotent: an unmounted container is
// reported without invoking pct; an unverifiable outcome reports unknown.
func (s *MountService) Unmount(ctx context.Context, vmid int, node string) (output.LXCMountResult, error) {
	result := output.LXCMountResult{Resource: "lxc", VMID: vmid}
	node = strings.TrimSpace(node)
	if node == "" {
		return result, fmt.Errorf("%w: --node is required for mount/unmount", ErrInvalid)
	}
	result.Node = node
	guest, err := s.resolveStopped(ctx, vmid, node, "pct unmount")
	if err != nil {
		return result, err
	}
	result.Node = guest.Row().Node
	path := lxcRootfsPath(vmid)
	result.Path = path

	mounted, err := s.pathMounted(ctx, path)
	if err != nil {
		return result, err
	}
	if !mounted {
		result.Status = "succeeded"
		result.Message = "rootfs is not mounted; nothing to do"
		return result, nil
	}

	run, err := s.runner.Run(ctx, nil, nil, nil, "pct", "unmount", strconv.Itoa(vmid))
	if err != nil {
		return result, err
	}
	if run.ExitCode != 0 {
		result.Status = "failed"
		result.Message = fmt.Sprintf("pct unmount exited with %d; manual cleanup may be required", run.ExitCode)
		return result, fmt.Errorf("%w: pct unmount %d failed: %s", ErrConflict, vmid, strings.TrimSpace(run.Stderr))
	}

	stillMounted, err := s.pathMounted(ctx, path)
	if err != nil {
		return result, err
	}
	result.Mounted = stillMounted
	if stillMounted {
		result.Status = "unknown"
		result.Message = "pct unmount returned success but the mount is still present; manual cleanup may be required"
		return result, nil
	}
	result.Status = "succeeded"
	result.Message = "rootfs unmounted; locks released"
	return result, nil
}

// resolveStopped requires the container to exist on the named node and be
// stopped: pct mount on a running container corrupts state. The SSH host
// binding is verified before any host command runs.
func (s *MountService) resolveStopped(ctx context.Context, vmid int, node, operation string) (Guest, error) {
	if vmid <= 0 {
		return nil, fmt.Errorf("%w: invalid vmid %d", ErrInvalid, vmid)
	}
	guest, err := s.guests.getOnNode(ctx, node, vmid)
	if err != nil {
		return nil, ClassifyError(err)
	}
	if guest.Row().Status != "stopped" {
		return nil, fmt.Errorf("%w: container %d is %s; stop it before mounting", ErrConflict, vmid, guest.Row().Status)
	}
	if err := requireSSHNodeBinding(s.sshNode, guest.Row().Node, vmid, operation); err != nil {
		return nil, err
	}
	return guest, nil
}

func (s *MountService) pathMounted(ctx context.Context, path string) (bool, error) {
	run, err := s.runner.Run(ctx, nil, nil, nil, "mountpoint", "-q", path)
	if err != nil {
		return false, err
	}
	return run.ExitCode == 0, nil
}

func lxcRootfsPath(vmid int) string {
	return fmt.Sprintf("/var/lib/lxc/%d/rootfs", vmid)
}
