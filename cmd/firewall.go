package cmd

import (
	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/output"
	"github.com/lz-wang/pvectl/internal/pve"
)

// newFirewallCommand is a read-only firewall inventory for node, VM, and LXC
// scopes. Firewall mutation is out of scope by design.
func newFirewallCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "firewall",
		Usage: "Inspect firewall status and rules (read-only)",
		Subcommands: []*cli.Command{
			firewallStatusCommand(deps),
			firewallListCommand(deps),
		},
	}
}

func firewallScopeFlags() []cli.Flag {
	return append(
		[]cli.Flag{
			&cli.StringFlag{Name: "node", Usage: "PVE node name", Required: true},
			&cli.StringFlag{Name: "scope", Value: pve.FirewallScopeNode, Usage: "firewall scope: node,vm,lxc"},
			&cli.IntFlag{Name: "vmid", Usage: "VMID/CTID; required when --scope is vm or lxc"},
		},
		commonOutputFlags()...,
	)
}

func parseFirewallScopeFromFlags(c *cli.Context) (pve.FirewallScope, error) {
	kind, err := pve.ParseFirewallScopeKind(c.String("scope"))
	if err != nil {
		return pve.FirewallScope{}, err
	}
	return pve.ParseFirewallScope(c.String("node"), kind, c.Int("vmid"))
}

func firewallStatusCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Show whether the firewall is enabled for a scope",
		Flags: firewallScopeFlags(),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}
			scope, err := parseFirewallScopeFromFlags(c)
			if err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			row, err := rt.backend.FirewallStatus(c.Context, scope)
			if err != nil {
				return err
			}
			return output.WriteFirewallStatus(rt.stdout, rt.format, row)
		},
	}
}

func firewallListCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "ls",
		Usage: "List firewall rules for a scope",
		Flags: firewallScopeFlags(),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}
			scope, err := parseFirewallScopeFromFlags(c)
			if err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			rows, err := rt.backend.FirewallRules(c.Context, scope)
			if err != nil {
				return err
			}
			return output.WriteFirewallRuleRows(rt.stdout, rt.format, rows)
		},
	}
}
