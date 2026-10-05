package cmd

import (
	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pve-cli/v2/internal/output"
	"github.com/lz-wang/pve-cli/v2/internal/pve"
)

func guestCloneCommand(kind string, deps Dependencies) *cli.Command {
	flags := []cli.Flag{
		&cli.StringFlag{Name: "node", Usage: "source PVE node name"},
		&cli.IntFlag{Name: "newid", Usage: "new VMID/CTID; omitted means automatic NextID"},
		&cli.StringFlag{Name: "target", Usage: "target PVE node name", Required: true},
		&cli.StringFlag{Name: "storage", Usage: "target storage"},
		&cli.BoolFlag{Name: "full", Usage: "perform a full clone"},
		&cli.StringFlag{Name: "pool", Usage: "target resource pool"},
		&cli.StringFlag{Name: "snapname", Usage: "snapshot name to clone from"},
		&cli.StringFlag{Name: "description", Usage: "description for the cloned guest"},
		&cli.BoolFlag{Name: "wait", Usage: "wait for async task completion"},
		&cli.DurationFlag{Name: "wait-timeout", Usage: "task wait timeout"},
		&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "output format: table,json,yaml"},
	}
	if kind == "vm" {
		flags = append(flags,
			&cli.StringFlag{Name: "name", Usage: "name for the cloned VM", Required: true},
			&cli.StringFlag{Name: "format", Usage: "target disk format"},
		)
	} else {
		flags = append(flags,
			&cli.StringFlag{Name: "hostname", Usage: "hostname for the cloned container", Required: true},
		)
	}

	return &cli.Command{
		Name:      "clone",
		Usage:     "Clone a guest",
		ArgsUsage: "SOURCE_VMID",
		Flags:     flags,
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
			result, err := guestService(kind, rt).Clone(c.Context, vmid, c.String("node"), pve.CloneOptions{
				NewID:       c.Int("newid"),
				Name:        c.String("name"),
				Hostname:    c.String("hostname"),
				Target:      c.String("target"),
				Storage:     c.String("storage"),
				Full:        c.Bool("full"),
				Pool:        c.String("pool"),
				SnapName:    c.String("snapname"),
				Description: c.String("description"),
				Format:      c.String("format"),
			})
			if err != nil {
				return err
			}
			return output.WriteCloneResult(rt.stdout, rt.format, result)
		},
	}
}
