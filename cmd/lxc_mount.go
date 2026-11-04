package cmd

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
)

// lxcMountCommand exposes controlled `pct mount` over the host SSH
// transport. These are emergency-maintenance operations: the container
// must be stopped, double mounts are refused, and there is no --force.
func lxcMountCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "mount",
		Usage:     "Mount a stopped container's rootfs on the host (emergency maintenance)",
		ArgsUsage: "VMID",
		Flags:     append(commonNodeFlag(), commonOutputFlags()...),
		Action:    runMountAction(deps, true),
	}
}

func lxcUnmountCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "unmount",
		Usage:     "Release a mounted container rootfs on the host",
		ArgsUsage: "VMID",
		Flags:     append(commonNodeFlag(), commonOutputFlags()...),
		Action:    runMountAction(deps, false),
	}
}

func runMountAction(deps Dependencies, mount bool) func(*cli.Context) error {
	return func(c *cli.Context) error {
		if err := requireNoExtraArgs(c, 1); err != nil {
			return err
		}
		node := c.String("node")
		if node == "" {
			return fmt.Errorf("%w: --node is required for mount/unmount", pve.ErrInvalid)
		}
		id, err := resolveGuestID(c, "lxc", deps, c.Args().First())
		if err != nil {
			return err
		}
		rt, err := buildRuntime(c, deps)
		if err != nil {
			return err
		}
		if rt.sshRunner == nil {
			return fmt.Errorf("%w: profile has no ssh section; add one to use lxc mount/unmount", pve.ErrInvalid)
		}
		service := pve.NewMountService(guestService("lxc", rt), rt.sshRunner, rt.sshNode)
		var result output.LXCMountResult
		if mount {
			result, err = service.Mount(c.Context, id, node)
		} else {
			result, err = service.Unmount(c.Context, id, node)
		}
		if writeErr := output.WriteLXCMountResult(rt.stdout, rt.format, result); writeErr != nil {
			return writeErr
		}
		return err
	}
}
