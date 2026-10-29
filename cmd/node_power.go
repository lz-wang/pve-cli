package cmd

import (
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/urfave/cli/v2"
)

func nodePowerCommand(action string, deps Dependencies) *cli.Command {
	return &cli.Command{Name: action, Usage: action + " a node after explicit confirmation", ArgsUsage: "NODE", Flags: append(commonOutputFlags(), &cli.BoolFlag{Name: "yes", Usage: "skip local node-name confirmation"}), Action: func(c *cli.Context) error {
		if err := requireNoExtraArgs(c, 1); err != nil {
			return err
		}
		rt, err := buildRuntime(c, deps)
		if err != nil {
			return err
		}
		node := c.Args().First()
		svc := pve.NewNodeService(rt.backend)
		plan, err := svc.PlanPower(c.Context, node, action)
		if err != nil {
			return err
		}
		impact := fmt.Sprintf("%s node %s: %d running VM(s), %d running LXC(s); API connectivity and guests may be interrupted; guests will not be stopped by this CLI", action, node, plan.RunningVM, plan.RunningLXC)
		if _, err = fmt.Fprintln(rt.stderr, impact); err != nil {
			return err
		}
		if !c.Bool("yes") {
			if err = confirmExact(deps.withDefaults().Stdin, rt.stderr, impact, node); err != nil {
				return err
			}
		}
		result, err := svc.Power(c.Context, node, action)
		if err != nil {
			return err
		}
		return output.WriteOperation(rt.stdout, rt.format, result)
	}}
}
