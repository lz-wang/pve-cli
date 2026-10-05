package cmd

import (
	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/output"
	"github.com/lz-wang/pvectl/internal/pve"
)

func newNodeCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "node",
		Usage: "Inspect PVE nodes",
		Subcommands: []*cli.Command{
			{
				Name:  "ls",
				Usage: "List nodes",
				Flags: commonOutputFlags(),
				Action: func(c *cli.Context) error {
					if err := requireNoExtraArgs(c, 0); err != nil {
						return err
					}
					rt, err := buildRuntime(c, deps)
					if err != nil {
						return err
					}
					rows, err := pve.NewNodeService(rt.backend).List(c.Context)
					if err != nil {
						return err
					}
					return output.WriteNodeRows(rt.stdout, rt.format, rows)
				},
			},
			{
				Name:      "get",
				Usage:     "Show node details",
				ArgsUsage: "NODE",
				Flags:     commonOutputFlags(),
				Action: func(c *cli.Context) error {
					if err := requireNoExtraArgs(c, 1); err != nil {
						return err
					}
					rt, err := buildRuntime(c, deps)
					if err != nil {
						return err
					}
					row, err := pve.NewNodeService(rt.backend).Get(c.Context, c.Args().First())
					if err != nil {
						return err
					}
					return output.WriteNodeDetail(rt.stdout, rt.format, row)
				},
			},
		},
	}
}
