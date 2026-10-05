package cmd

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/output"
	"github.com/lz-wang/pvectl/internal/pve"
)

func newDoctorCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "doctor",
		Usage: "Diagnose pve config and Proxmox API connectivity",
		Flags: append(
			[]cli.Flag{
				&cli.BoolFlag{Name: "offline", Usage: "check local config only without connecting to Proxmox VE"},
				&cli.StringFlag{Name: "node", Usage: "verify that a node exists during online checks"},
			},
			commonOutputFlags()...,
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}

			outputSet := flagIsSet(c, "output")
			outputFormat := stringFlag(c, "output")
			if outputSet {
				outputFormat = output.NormalizeFormat(outputFormat)
				if err := output.ValidateFormat(outputFormat); err != nil {
					return err
				}
			}

			result := pve.NewDoctorService(deps.BackendFactory).Run(c.Context, pve.DoctorOptions{
				ConfigPath:  c.String("config"),
				ProfileName: c.String("profile"),
				Offline:     c.Bool("offline"),
				Node:        c.String("node"),
				Timeout:     durationFlag(c, "api-timeout"),
				Insecure:    boolFlag(c, "insecure"),
				Output:      outputFormat,
				OutputSet:   outputSet,
			})

			if err := output.WriteDoctorRows(c.App.Writer, result.Format, result.Rows); err != nil {
				return err
			}
			if result.Failed {
				return fmt.Errorf("doctor checks failed")
			}
			return nil
		},
	}
}
