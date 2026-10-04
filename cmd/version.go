package cmd

import (
	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/output"
)

func newVersionCommand(info BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "Show pve build and runtime version information",
		Flags: commonOutputFlags(),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}
			format := output.NormalizeFormat(stringFlag(c, "output"))
			if err := output.ValidateFormat(format); err != nil {
				return err
			}
			return output.WriteVersionInfo(c.App.Writer, format, info.versionInfo())
		},
	}
}
