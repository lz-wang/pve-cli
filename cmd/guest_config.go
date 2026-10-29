package cmd

import (
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/urfave/cli/v2"
)

func guestConfigCommand(kind string, deps Dependencies) *cli.Command {
	return &cli.Command{Name: "config", Usage: "Read or update native guest configuration", Subcommands: []*cli.Command{
		{Name: "get", Usage: "Read native PVE configuration (credentials redacted)", ArgsUsage: "VMID", Flags: append(commonNodeFlag(), commonOutputFlags()...), Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			id, err := resolveGuestID(c, kind, deps, c.Args().First())
			if err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			svc := guestService(kind, rt)
			row, err := svc.Get(c.Context, id, c.String("node"))
			if err != nil {
				return err
			}
			cfg, err := svc.NativeConfig(c.Context, id, row.Node)
			if err != nil {
				return err
			}
			return output.WriteGuestConfig(rt.stdout, rt.format, output.GuestConfig{Resource: kind, VMID: id, Node: row.Node, Config: cfg})
		}},
		{Name: "set", Usage: "Set native PVE configuration", ArgsUsage: "VMID", Flags: append(commonControlFlags(), &cli.StringSliceFlag{Name: "set", Usage: "repeatable key=value"}), Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			values, err := parseSetFlags(c.StringSlice("set"))
			if err != nil {
				return err
			}
			id, err := resolveGuestID(c, kind, deps, c.Args().First())
			if err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			rt.tasks.ResultWriter = rt.stdout
			return guestService(kind, rt).Config(c.Context, id, c.String("node"), values)
		}},
	}}
}
