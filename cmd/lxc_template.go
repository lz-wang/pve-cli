package cmd

import (
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/urfave/cli/v2"
)

func newLXCTemplateCommand(deps Dependencies) *cli.Command {
	return &cli.Command{Name: "template", Usage: "Manage traditional system container templates", Subcommands: []*cli.Command{
		templateDownloadCommand(deps),
		{Name: "ls", Usage: "List available or downloaded system templates", Flags: append([]cli.Flag{
			&cli.StringFlag{Name: "node", Required: true, Usage: "PVE node"}, &cli.StringFlag{Name: "storage", Usage: "storage for downloaded templates"}, &cli.BoolFlag{Name: "available", Usage: "query downloadable index"}, &cli.StringFlag{Name: "section", Value: "system"}, &cli.StringFlag{Name: "name", Usage: "name substring"},
		}, commonOutputFlags()...), Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			rows, err := pve.NewTemplateService(rt.backend, rt.tasks).List(c.Context, pve.TemplateOptions{Node: c.String("node"), Storage: c.String("storage"), Section: c.String("section"), Name: c.String("name"), Available: c.Bool("available")})
			if err != nil {
				return err
			}
			return output.WriteTemplates(rt.stdout, rt.format, rows)
		}},
	}}
}

func templateDownloadCommand(deps Dependencies) *cli.Command {
	return &cli.Command{Name: "download", Usage: "Download a system template", ArgsUsage: "NAME", Flags: append(commonControlFlags(), &cli.StringFlag{Name: "storage", Required: true}), Action: func(c *cli.Context) error {
		if err := requireNoExtraArgs(c, 1); err != nil {
			return err
		}
		rt, err := buildRuntime(c, deps)
		if err != nil {
			return err
		}
		result, err := pve.NewTemplateService(rt.backend, rt.tasks).Download(c.Context, c.String("node"), c.String("storage"), c.Args().First())
		if result.Task != "" {
			if e := output.WriteOperation(rt.stdout, rt.format, result); e != nil {
				return e
			}
		}
		return err
	}}
}
