package cmd

import (
	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/pve"
)

func newVMCommand(deps Dependencies) *cli.Command {
	return newGuestCommand("vm", "Manage QEMU virtual machines", deps)
}

func newLXCCommand(deps Dependencies) *cli.Command {
	return newGuestCommand("lxc", "Manage LXC containers", deps)
}

func newGuestCommand(kind, usage string, deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  kind,
		Usage: usage,
		Subcommands: []*cli.Command{
			guestListCommand(kind, deps),
			guestGetCommand(kind, deps),
			guestControlCommand(kind, "start", "Start a guest", deps),
			guestControlCommand(kind, "shutdown", "Shutdown a guest gracefully", deps),
			guestControlCommand(kind, "stop", "Stop a guest immediately", deps),
			guestControlCommand(kind, "reboot", "Reboot a guest", deps),
			guestCloneCommand(kind, deps),
			guestConfigCommand(kind, deps),
			guestDeleteCommand(kind, deps),
			guestMigrateCommand(kind, deps),
			guestResizeCommand(kind, deps),
			guestSnapshotCommand(kind, deps),
			guestBackupCommand(kind, deps),
			guestRestoreCommand(kind, deps),
		},
	}
}

func guestService(kind string, rt *runtime) *pve.GuestService {
	if kind == "vm" {
		return pve.NewVMService(rt.backend, rt.tasks, rt.logger, rt.verbose)
	}
	return pve.NewLXCService(rt.backend, rt.tasks, rt.logger, rt.verbose)
}
