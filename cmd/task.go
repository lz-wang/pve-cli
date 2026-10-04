package cmd

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/output"
	"github.com/lz-wang/pvectl/internal/pve"
)

func newTaskCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "task",
		Usage: "Inspect and wait for PVE tasks",
		Subcommands: []*cli.Command{
			taskListCommand(deps),
			taskGetCommand(deps),
			taskLogCommand(deps),
			taskWaitCommand(deps),
		},
	}
}

func taskService(rt *runtime) *pve.TaskService {
	return pve.NewTaskService(rt.backend, rt.logger, rt.verbose)
}

func taskListCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "ls",
		Usage: "List recent tasks across nodes",
		Flags: append(
			[]cli.Flag{
				&cli.StringFlag{Name: "node", Usage: "PVE node name"},
				&cli.StringFlag{Name: "type", Usage: "filter by task type, for example vzdump"},
				&cli.StringFlag{Name: "status", Usage: "filter by task status: running,ok,error"},
				&cli.IntFlag{Name: "limit", Usage: "show only the N most recent tasks"},
			},
			commonOutputFlags()...,
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}
			status, err := pve.ParseTaskStatus(c.String("status"))
			if err != nil {
				return err
			}
			if limit := c.Int("limit"); limit < 0 {
				return fmt.Errorf("invalid limit %d", limit)
			}

			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			rows, err := taskService(rt).List(c.Context, pve.TaskListOptions{
				Node:   c.String("node"),
				Type:   c.String("type"),
				Status: status,
				Limit:  c.Int("limit"),
			})
			if err != nil {
				return err
			}
			return output.WriteTaskRows(rt.stdout, rt.format, rows)
		},
	}
}

func taskGetCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "get",
		Usage:     "Show task status",
		ArgsUsage: "UPID",
		Flags:     commonOutputFlags(),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			row, err := taskService(rt).Get(c.Context, c.Args().First())
			if err != nil {
				return err
			}
			return output.WriteTaskDetail(rt.stdout, rt.format, row)
		},
	}
}

func taskLogCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "log",
		Usage:     "Show task log output",
		ArgsUsage: "UPID",
		Flags: append(
			[]cli.Flag{
				&cli.IntFlag{Name: "tail", Usage: "show only the last N log lines"},
			},
			commonOutputFlags()...,
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			tail := c.Int("tail")
			if tail < 0 {
				return fmt.Errorf("invalid tail %d", tail)
			}

			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			rows, err := taskService(rt).Log(c.Context, c.Args().First(), pve.TaskLogOptions{Tail: tail})
			if err != nil {
				return err
			}
			return output.WriteTaskLogRows(rt.stdout, rt.format, rows)
		},
	}
}

func taskWaitCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "wait",
		Usage:     "Wait for a task to complete",
		ArgsUsage: "UPID",
		Flags: append(
			[]cli.Flag{
				&cli.DurationFlag{Name: "wait-timeout", Usage: "task wait timeout"},
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
			upid := c.Args().First()
			fmt.Fprintf(rt.stderr, "waiting for task: %s\n", upid)
			row, err := taskService(rt).Wait(c.Context, upid, rt.tasks.WaitTimeout)
			if row.UPID != "" {
				if writeErr := output.WriteTaskDetail(rt.stdout, rt.format, row); writeErr != nil {
					return writeErr
				}
			}
			return err
		},
	}
}
