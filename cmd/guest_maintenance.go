package cmd

import (
	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pve-cli/v2/internal/pve"
)

func guestMigrateCommand(kind string, deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "migrate",
		Usage:     "Migrate a guest to another node",
		ArgsUsage: "VMID",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "node", Usage: "source PVE node name"},
			&cli.StringFlag{Name: "target", Usage: "target PVE node name", Required: true},
			&cli.BoolFlag{Name: "online", Usage: "request online migration"},
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
			return guestService(kind, rt).Migrate(c.Context, vmid, c.String("node"), pve.MigrateOptions{
				Target: c.String("target"),
				Online: c.Bool("online"),
			})
		},
	}
}

func guestResizeCommand(kind string, deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "resize",
		Usage:     "Resize a guest disk",
		ArgsUsage: "VMID",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "node", Usage: "PVE node name"},
			&cli.StringFlag{Name: "disk", Usage: "disk identifier", Required: true},
			&cli.StringFlag{Name: "size", Usage: "new size or increment, for example +20G", Required: true},
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
			return guestService(kind, rt).Resize(c.Context, vmid, c.String("node"), c.String("disk"), c.String("size"))
		},
	}
}
