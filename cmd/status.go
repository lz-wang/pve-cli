package cmd

import (
	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pve-cli/v2/internal/output"
	"github.com/lz-wang/pve-cli/v2/internal/pve"
)

func newStatusCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Show HomeLab status overview across nodes, guests, storage, and backups",
		Flags: commonOutputFlags(),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			report, err := pve.NewStatusService(rt.backend).Report(c.Context)
			if err != nil {
				return err
			}
			return output.WriteStatusReport(rt.stdout, rt.format, report)
		},
	}
}
