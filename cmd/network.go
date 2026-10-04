package cmd

import (
	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/output"
	"github.com/lz-wang/pvectl/internal/pve"
)

// newNetworkCommand is read-only network inventory. Mutating PVE network
// configuration remotely is out of scope by design.
func newNetworkCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "network",
		Usage: "Inspect node network interfaces (read-only)",
		Subcommands: []*cli.Command{
			networkListCommand(deps),
			networkGetCommand(deps),
		},
	}
}

func networkListCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "ls",
		Usage: "List network interfaces",
		Flags: append(
			[]cli.Flag{
				&cli.StringFlag{Name: "node", Usage: "PVE node name"},
				&cli.StringFlag{Name: "type", Usage: "filter by interface type, for example bridge, bond, or vlan"},
				&cli.BoolFlag{Name: "active", Usage: "show only active interfaces"},
			},
			commonOutputFlags()...,
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			rows, err := pve.NewNetworkService(rt.backend, rt.logger, rt.verbose).List(c.Context, c.String("node"), pve.NetworkListOptions{
				Type:   c.String("type"),
				Active: c.Bool("active"),
			})
			if err != nil {
				return err
			}
			return output.WriteNetworkRows(rt.stdout, rt.format, rows)
		},
	}
}

func networkGetCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "get",
		Usage:     "Show network interface details",
		ArgsUsage: "IFACE",
		Flags: append(
			[]cli.Flag{
				&cli.StringFlag{Name: "node", Usage: "PVE node name", Required: true},
			},
			commonOutputFlags()...,
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			row, err := pve.NewNetworkService(rt.backend, rt.logger, rt.verbose).Get(c.Context, c.String("node"), c.Args().First())
			if err != nil {
				return err
			}
			return output.WriteNetworkDetail(rt.stdout, rt.format, row)
		},
	}
}
