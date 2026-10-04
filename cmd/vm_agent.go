package cmd

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/output"
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
			if err := rt.backend.AgentPing(c.Context, c.String("node"), vmid); err != nil {
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
			rows, err := rt.backend.AgentNetwork(c.Context, c.String("node"), vmid)
			if err != nil {
				return err
			}
			return output.WriteAgentNetworkRows(rt.stdout, rt.format, rows)
		},
	}
}
