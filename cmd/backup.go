package cmd

import (
	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/output"
	"github.com/lz-wang/pvectl/internal/pve"
)

func newBackupCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "backup",
		Usage: "List backup files",
		Subcommands: []*cli.Command{
			backupListCommand(deps),
		},
	}
}

func backupListCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "ls",
		Usage: "List backup files on a storage",
		Flags: append(
			[]cli.Flag{
				&cli.StringFlag{Name: "node", Usage: "PVE node name", Required: true},
				&cli.StringFlag{Name: "storage", Usage: "backup storage name", Required: true},
				&cli.IntFlag{Name: "vmid", Usage: "filter by VMID/CTID"},
				&cli.StringFlag{Name: "type", Value: pve.BackupKindAll, Usage: "backup type: all,vm,lxc"},
				&cli.BoolFlag{Name: "latest", Usage: "show only latest backup per guest"},
			},
			commonOutputFlags()...,
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}
			kind, err := pve.ParseBackupKind(c.String("type"))
			if err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			rows, err := pve.NewBackupService(rt.backend, rt.tasks).List(c.Context, pve.BackupListOptions{
				Node:    c.String("node"),
				Storage: c.String("storage"),
				VMID:    c.Int("vmid"),
				Kind:    kind,
				Latest:  c.Bool("latest"),
			})
			if err != nil {
				return err
			}
			return output.WriteBackupRows(rt.stdout, rt.format, rows)
		},
	}
}
