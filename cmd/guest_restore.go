package cmd

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/output"
	"github.com/lz-wang/pvectl/internal/pve"
)

func guestRestoreCommand(kind string, deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "restore",
		Usage:     "Restore a backup archive into a new, non-existing guest",
		ArgsUsage: "ARCHIVE",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "node", Usage: "target PVE node name", Required: true},
			&cli.IntFlag{Name: "vmid", Usage: "new VMID/CTID; it must not exist yet", Required: true},
			&cli.StringFlag{Name: "storage", Usage: "target storage for guest disks"},
			&cli.BoolFlag{Name: "wait", Usage: "wait for async task completion"},
			&cli.DurationFlag{Name: "wait-timeout", Usage: "task wait timeout"},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "output format: table,json,yaml"},
		},
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			vmid := c.Int("vmid")
			if vmid <= 0 {
				return fmt.Errorf("invalid vmid %d", vmid)
			}

			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			result, err := pve.NewRestoreService(rt.backend, rt.tasks, rt.logger, rt.verbose).Restore(c.Context, pve.RestoreOptions{
				Kind:    kind,
				Archive: c.Args().First(),
				Node:    c.String("node"),
				VMID:    vmid,
				Storage: c.String("storage"),
			})
			if err != nil {
				return err
			}
			return output.WriteRestoreResult(rt.stdout, rt.format, result)
		},
	}
}
