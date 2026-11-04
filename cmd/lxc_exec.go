package cmd

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
)

// newLXCExecCommand runs commands inside LXC containers through the host SSH
// transport (`pct exec`). The REST API has no equivalent endpoint, so this
// command requires a profile with an ssh section.
func newLXCExecCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "exec",
		Usage:     "Run a command inside an LXC container via the host SSH transport",
		ArgsUsage: "VMID -- COMMAND [ARGS...]",
		Flags: append(append(commonNodeFlag(), commonOutputFlags()...),
			&cli.BoolFlag{Name: "stdin", Usage: "forward standard input to the container command"},
			&cli.DurationFlag{Name: "timeout", Usage: "terminate the command after this duration"},
		),
		Action: func(c *cli.Context) error {
			if c.NArg() < 2 {
				return fmt.Errorf("%w: expected VMID -- COMMAND [ARGS...]", pve.ErrInvalid)
			}
			args := c.Args().Slice()
			command, arguments := splitRemoteCommand(args[1:])
			if command == "" {
				return fmt.Errorf("%w: a command is required after --", pve.ErrInvalid)
			}
			if err := output.ValidateFormat(output.NormalizeFormat(c.String("output"))); err != nil {
				return err
			}
			if c.IsSet("timeout") && c.Duration("timeout") <= 0 {
				return fmt.Errorf("%w: timeout must be positive", pve.ErrInvalid)
			}

			id, err := resolveGuestID(c, "lxc", deps, args[0])
			if err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			if rt.sshRunner == nil {
				return fmt.Errorf("%w: profile has no ssh section; add one to use lxc exec", pve.ErrInvalid)
			}

			options := pve.LXCExecOptions{
				Node:    c.String("node"),
				Command: command,
				Args:    arguments,
				Timeout: c.Duration("timeout"),
			}
			if c.Bool("stdin") {
				options.Stdin = deps.Stdin
			}
			if output.NormalizeFormat(c.String("output")) == output.FormatTable {
				options.Stdout = rt.stdout
				options.Stderr = rt.stderr
			}

			result, err := pve.NewLXCExecService(guestService("lxc", rt), rt.sshRunner, rt.sshNode).Exec(c.Context, id, options)
			if output.NormalizeFormat(c.String("output")) != output.FormatTable {
				if writeErr := output.WriteAgentExecResult(rt.stdout, rt.format, result); writeErr != nil {
					return writeErr
				}
			}
			return err
		},
	}
}

// splitRemoteCommand accepts the raw argument tail (with or without the
// leading --) and returns the command and its arguments.
func splitRemoteCommand(args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}
	if args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return "", nil
	}
	return args[0], args[1:]
}
