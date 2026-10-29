package cmd

import "github.com/urfave/cli/v2"

func guestDeleteCommand(kind string, deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "delete",
		Usage:     "Delete a guest",
		ArgsUsage: "VMID",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "node", Usage: "PVE node name"},
			&cli.BoolFlag{Name: "yes", Usage: "skip local delete confirmation"},
			&cli.BoolFlag{Name: "wait", Usage: "wait for async task completion"},
			&cli.DurationFlag{Name: "wait-timeout", Usage: "task wait timeout"},
		},
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
			svc := guestService(kind, rt)
			if !c.Bool("yes") {
				row, err := svc.Get(c.Context, vmid, c.String("node"))
				if err != nil {
					return err
				}
				if err := confirmDelete(deps.withDefaults().Stdin, rt.stderr, kind, row.VMID, row.Node); err != nil {
					return err
				}
			}
			return svc.Delete(c.Context, vmid, c.String("node"))
		},
	}
}
