package cmd

import (
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/urfave/cli/v2"
)

func taskCancelCommand(deps Dependencies) *cli.Command {
	return &cli.Command{Name: "cancel", Usage: "Request cancellation of a running task", ArgsUsage: "UPID", Flags: append(commonOutputFlags(), &cli.BoolFlag{Name: "yes", Usage: "skip local confirmation"}), Action: func(c *cli.Context) error {
		if err := requireNoExtraArgs(c, 1); err != nil {
			return err
		}
		upid := c.Args().First()
		if _, err := pve.ParseUPID(upid); err != nil {
			return err
		}
		rt, err := buildRuntime(c, deps)
		if err != nil {
			return err
		}
		row, err := taskService(rt).Get(c.Context, upid)
		if err != nil {
			return err
		}
		if row.Status != pve.TaskStatusRunning {
			return fmt.Errorf("%w: task is not running", pve.ErrConflict)
		}
		if !c.Bool("yes") {
			if err := confirmExact(deps.withDefaults().Stdin, rt.stderr, fmt.Sprintf("Cancel %s resource %s on %s", row.Type, row.ID, row.Node), upid); err != nil {
				return err
			}
		}
		result, err := taskService(rt).Cancel(c.Context, upid)
		if result.Task != "" {
			if e := output.WriteOperation(rt.stdout, rt.format, result); e != nil {
				return e
			}
		}
		return err
	}}
}
