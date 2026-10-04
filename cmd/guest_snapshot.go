package cmd

import (
	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/output"
)

func guestSnapshotCommand(kind string, deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "snapshot",
		Usage: "Manage guest snapshots",
		Subcommands: []*cli.Command{
			{
				Name:      "ls",
				Usage:     "List snapshots",
				ArgsUsage: "VMID",
				Flags:     append(commonNodeFlag(), commonOutputFlags()...),
				Action: func(c *cli.Context) error {
					if err := requireNoExtraArgs(c, 1); err != nil {
						return err
					}
					vmid, err := parseVMID(c.Args().First())
					if err != nil {
						return err
					}
					rt, err := buildRuntime(c, deps)
					if err != nil {
						return err
					}
					rows, err := guestService(kind, rt).ListSnapshots(c.Context, vmid, c.String("node"))
					if err != nil {
						return err
					}
					return output.WriteSnapshotRows(rt.stdout, rt.format, rows)
				},
			},
			{
				Name:      "create",
				Usage:     "Create a snapshot",
				ArgsUsage: "VMID SNAPNAME",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "node", Usage: "PVE node name"},
					&cli.BoolFlag{Name: "wait", Usage: "wait for async task completion"},
					&cli.DurationFlag{Name: "wait-timeout", Usage: "task wait timeout"},
				},
				Action: func(c *cli.Context) error {
					if err := requireNoExtraArgs(c, 2); err != nil {
						return err
					}
					vmid, err := parseVMID(c.Args().Get(0))
					if err != nil {
						return err
					}
					snapshotName, err := parseSnapshotName(c.Args().Get(1))
					if err != nil {
						return err
					}
					rt, err := buildRuntime(c, deps)
					if err != nil {
						return err
					}
					return guestService(kind, rt).CreateSnapshot(c.Context, vmid, c.String("node"), snapshotName)
				},
			},
			{
				Name:      "rollback",
				Usage:     "Rollback to a snapshot",
				ArgsUsage: "VMID SNAPNAME",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "node", Usage: "PVE node name"},
					&cli.BoolFlag{Name: "force", Usage: "skip local rollback confirmation"},
					&cli.BoolFlag{Name: "wait", Usage: "wait for async task completion"},
					&cli.DurationFlag{Name: "wait-timeout", Usage: "task wait timeout"},
				},
				Action: func(c *cli.Context) error {
					if err := requireNoExtraArgs(c, 2); err != nil {
						return err
					}
					vmid, err := parseVMID(c.Args().Get(0))
					if err != nil {
						return err
					}
					snapshotName, err := parseSnapshotName(c.Args().Get(1))
					if err != nil {
						return err
					}
					rt, err := buildRuntime(c, deps)
					if err != nil {
						return err
					}
					svc := guestService(kind, rt)
					if !c.Bool("force") {
						row, err := svc.Get(c.Context, vmid, c.String("node"))
						if err != nil {
							return err
						}
						if err := confirmRollback(deps.withDefaults().Stdin, rt.stderr, kind, row.VMID, row.Node, snapshotName); err != nil {
							return err
						}
					}
					return svc.RollbackSnapshot(c.Context, vmid, c.String("node"), snapshotName)
				},
			},
			{
				Name:      "delete",
				Usage:     "Delete a snapshot",
				ArgsUsage: "VMID SNAPNAME",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "node", Usage: "PVE node name"},
					&cli.BoolFlag{Name: "force", Usage: "skip local snapshot delete confirmation"},
					&cli.BoolFlag{Name: "wait", Usage: "wait for async task completion"},
					&cli.DurationFlag{Name: "wait-timeout", Usage: "task wait timeout"},
				},
				Action: func(c *cli.Context) error {
					if err := requireNoExtraArgs(c, 2); err != nil {
						return err
					}
					vmid, err := parseVMID(c.Args().Get(0))
					if err != nil {
						return err
					}
					snapshotName, err := parseSnapshotName(c.Args().Get(1))
					if err != nil {
						return err
					}
					rt, err := buildRuntime(c, deps)
					if err != nil {
						return err
					}
					svc := guestService(kind, rt)
					if !c.Bool("force") {
						row, err := svc.Get(c.Context, vmid, c.String("node"))
						if err != nil {
							return err
						}
						if err := confirmSnapshotDelete(deps.withDefaults().Stdin, rt.stderr, kind, row.VMID, row.Node, snapshotName); err != nil {
							return err
						}
					}
					return svc.DeleteSnapshot(c.Context, vmid, c.String("node"), snapshotName)
				},
			},
		},
	}
}
