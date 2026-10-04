package cmd

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/output"
	"github.com/lz-wang/pvectl/internal/pve"
)

// guestBulkCommand builds the bulk lifecycle commands under `guest`.
// Selection is shared between actions; execution semantics live in
// runBulkGuestAction.
func guestBulkCommand(action, usage string, deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  action,
		Usage: usage,
		Flags: append(
			[]cli.Flag{
				&cli.StringFlag{Name: "node", Usage: "limit to one PVE node"},
				&cli.StringFlag{Name: "type", Value: "all", Usage: "guest type: all,vm,lxc"},
				&cli.StringFlag{Name: "status", Usage: "guest status filter, for example running or stopped"},
				&cli.StringSliceFlag{Name: "tag", Usage: "filter guests by tag, repeatable"},
				&cli.StringFlag{Name: "tag-match", Value: pve.TagMatchAll, Usage: "tag match mode: all,any"},
				&cli.BoolFlag{Name: "dry-run", Usage: "show the guests that would be affected and exit"},
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
			selector := pve.GuestSelector{
				Node:     c.String("node"),
				Type:     guestType,
				Status:   c.String("status"),
				Tags:     c.StringSlice("tag"),
				TagMatch: tagMatch,
			}

			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			svc := pve.NewBulkService(rt.backend, rt.tasks, rt.logger, rt.verbose)
			rows, err := svc.Select(c.Context, selector)
			if err != nil {
				return err
			}

			if c.Bool("dry-run") {
				return output.WriteBulkPlan(rt.stdout, rt.format, rows)
			}
			return fmt.Errorf("bulk %s execution requires --dry-run to preview first; refusing to run", action)
		},
	}
}
