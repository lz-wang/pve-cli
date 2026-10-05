package cmd

import (
	"fmt"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pve-cli/v2/internal/output"
	"github.com/lz-wang/pve-cli/v2/internal/pve"
)

// newVMAgentCommand exposes read-mostly QEMU guest agent operations. Only VMs
// expose the agent API; LXC containers are intentionally not supported here.
func newVMAgentCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "agent",
		Usage: "Query the QEMU guest agent of a VM",
		Subcommands: []*cli.Command{
			vmAgentPingCommand(deps),
			vmAgentNetworkCommand(deps),
			vmAgentExecCommand(deps),
		},
	}
}

func vmAgentPingCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "ping",
		Usage:     "Check that the guest agent answers",
		ArgsUsage: "VMID",
		Flags:     commonNodeFlag(),
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
			if err := pve.NewAgentService(rt.backend, rt.logger, rt.verbose).Ping(c.Context, vmid, c.String("node")); err != nil {
				return err
			}
			fmt.Fprintln(rt.stdout, "agent ok")
			return nil
		},
	}
}

func vmAgentNetworkCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "network",
		Usage:     "Show network interfaces reported by the guest agent",
		ArgsUsage: "VMID",
		Flags: append(
			[]cli.Flag{
				&cli.StringFlag{Name: "node", Usage: "PVE node name"},
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
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			rows, err := pve.NewAgentService(rt.backend, rt.logger, rt.verbose).Network(c.Context, vmid, c.String("node"))
			if err != nil {
				return err
			}
			return output.WriteAgentNetworkRows(rt.stdout, rt.format, rows)
		},
	}
}

func vmAgentExecCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "exec",
		Usage:     "Run executable+argv inside the guest via the agent",
		ArgsUsage: "VMID -- COMMAND [ARG...]",
		Flags: append(
			[]cli.Flag{
				&cli.StringFlag{Name: "node", Usage: "PVE node name"},
				&cli.StringFlag{Name: "input", Usage: "stdin data passed to the command"},
				&cli.DurationFlag{Name: "timeout", Usage: "how long to wait for the command to exit"},
			},
			commonOutputFlags()...,
		),
		Action: func(c *cli.Context) error {
			if c.NArg() < 2 {
				return fmt.Errorf("expected VMID and COMMAND, for example: pve vm agent exec 100 -- /usr/bin/uname -a")
			}
			vmid, err := parseVMID(c.Args().First())
			if err != nil {
				return err
			}
			command := c.Args().Slice()[1:]
			if command[0] == "--" {
				command = command[1:]
			}
			if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
				return fmt.Errorf("agent exec requires an executable path, for example -- /usr/bin/uname -a")
			}

			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			result, err := pve.NewAgentService(rt.backend, rt.logger, rt.verbose).Exec(c.Context, vmid, c.String("node"), pve.AgentExecOptions{
				Command: command,
				Input:   c.String("input"),
				Timeout: durationFlag(c, "timeout"),
			})
			if err != nil {
				return err
			}
			if err := output.WriteAgentExecResult(rt.stdout, rt.format, result); err != nil {
				return err
			}
			if result.ExitCode != 0 {
				return fmt.Errorf("agent exec exited with code %d", result.ExitCode)
			}
			return nil
		},
	}
}
