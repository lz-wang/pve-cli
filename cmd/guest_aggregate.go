package cmd

import (
	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/output"
	"github.com/lz-wang/pvectl/internal/pve"
)

func newGuestAggregateCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "guest",
		Usage: "Inspect and operate on VM/QEMU and LXC guests",
		Subcommands: []*cli.Command{
			guestAggregateListCommand(deps),
			guestAggregateGetCommand(deps),
			guestBulkCommand("start", "Start matching guests in bulk", deps),
			guestBulkCommand("shutdown", "Shutdown matching guests in bulk", deps),
			guestBulkCommand("reboot", "Reboot matching guests in bulk", deps),
			guestBulkCommand("stop", "Stop matching guests in bulk", deps),
		},
	}
}

func guestAggregateListCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "ls",
		Usage: "List VM/QEMU and LXC guests",
		Flags: append(
			[]cli.Flag{
				&cli.StringFlag{Name: "node", Usage: "PVE node name"},
				&cli.StringFlag{Name: "type", Value: "all", Usage: "guest type: all,vm,lxc"},
				&cli.StringFlag{Name: "status", Usage: "guest status filter, for example running or stopped"},
				&cli.StringSliceFlag{Name: "tag", Usage: "filter guests by tag, repeatable"},
				&cli.StringFlag{Name: "tag-match", Value: pve.TagMatchAll, Usage: "tag match mode: all,any"},
			},
			commonOutputFlags()...,
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}

			guestType, err := pve.ParseGuestListType(c.String("type"))
			if err != nil {
				return err
			}
			tagMatch, err := pve.ParseTagMatch(c.String("tag-match"))
			if err != nil {
				return err
			}

			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}

			svc := pve.NewGuestAggregateService(rt.backend, rt.logger, rt.verbose)
			rows, err := svc.List(c.Context, pve.GuestListOptions{
				Node:     c.String("node"),
				Type:     guestType,
				Status:   c.String("status"),
				Tags:     c.StringSlice("tag"),
				TagMatch: tagMatch,
			})
			if err != nil {
				return err
			}
			return output.WriteGuestRowsWithKind(rt.stdout, rt.format, rows)
		},
	}
}

func guestAggregateGetCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "get",
		Usage:     "Show VM/QEMU or LXC guest details",
		ArgsUsage: "VMID",
		Flags: append(
			[]cli.Flag{
				&cli.StringFlag{Name: "node", Usage: "PVE node name"},
				&cli.StringFlag{Name: "type", Value: "auto", Usage: "guest type: auto,vm,lxc"},
			},
			commonOutputFlags()...,
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}

			vmid, err := parseVMID(c.Args().First())
			if err != nil {
				return err
			}
			guestType, err := pve.ParseGuestGetType(c.String("type"))
			if err != nil {
				return err
			}

			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}

			svc := pve.NewGuestAggregateService(rt.backend, rt.logger, rt.verbose)
			row, err := svc.Get(c.Context, vmid, pve.GuestGetOptions{
				Node: c.String("node"),
				Type: guestType,
			})
			if err != nil {
				return err
			}
			return output.WriteGuestDetail(rt.stdout, rt.format, row)
		},
	}
}
