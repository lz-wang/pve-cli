package cmd

import "github.com/urfave/cli/v2"

func guestConfigCommand(kind string, deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "config",
		Usage:     "Update guest config with generic key=value options",
		ArgsUsage: "VMID",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "node", Usage: "PVE node name"},
			&cli.StringSliceFlag{Name: "set", Usage: "config option in key=value form"},
			&cli.BoolFlag{Name: "wait", Usage: "wait for async task completion"},
			&cli.DurationFlag{Name: "wait-timeout", Usage: "task wait timeout"},
		},
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			vmid, err := resolveGuestID(c, kind, deps, c.Args().First())
			if err != nil {
				return err
			}
			values, err := parseSetFlags(c.StringSlice("set"))
			if err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			return guestService(kind, rt).Config(c.Context, vmid, c.String("node"), values)
		},
	}
}
