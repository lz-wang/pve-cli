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
				&cli.IntFlag{Name: "jobs", Value: 2, Usage: "number of guests to operate on concurrently"},
				&cli.BoolFlag{Name: "dry-run", Usage: "show the guests that would be affected and exit"},
				&cli.BoolFlag{Name: "force", Usage: "skip local bulk confirmation"},
				&cli.BoolFlag{Name: "wait", Usage: "wait for async task completion"},
				&cli.DurationFlag{Name: "wait-timeout", Usage: "task wait timeout"},
			},
			commonOutputFlags()...,
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}
			if jobs := c.Int("jobs"); jobs < 0 {
				return fmt.Errorf("invalid jobs %d", jobs)
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
			if len(rows) == 0 {
				return fmt.Errorf("no guests match the selection")
			}
			if len(rows) > 1 && !c.Bool("force") {
				if err := confirmBulkAction(deps.withDefaults().Stdin, rt.stderr, action, rows); err != nil {
					return err
				}
			}
			return svc.ExecuteRows(c.Context, action, rows, pve.BulkExecuteOptions{
				Action:      action,
				Jobs:        c.Int("jobs"),
				Wait:        boolFlag(c, "wait"),
				WaitTimeout: durationFlag(c, "wait-timeout"),
				ErrWriter:   rt.stderr,
			})
		},
	}
}
