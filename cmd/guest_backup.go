package cmd

import (
	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/output"
	"github.com/lz-wang/pvectl/internal/pve"
)

func guestBackupCommand(kind string, deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "backup",
		Usage:     "Create a backup for a guest",
		ArgsUsage: "VMID",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "node", Usage: "PVE node name"},
			&cli.StringFlag{Name: "storage", Usage: "backup storage name", Required: true},
			&cli.StringFlag{Name: "mode", Value: pve.BackupModeSnapshot, Usage: "backup mode: snapshot,suspend,stop"},
			&cli.StringFlag{Name: "compress", Value: pve.BackupCompressZstd, Usage: "compression: zstd,lzo,gzip,none"},
			&cli.StringFlag{Name: "notes-template", Usage: "backup notes template"},
			&cli.UintFlag{Name: "bwlimit", Usage: "bandwidth limit in KiB/s"},
			&cli.BoolFlag{Name: "protected", Usage: "protect the backup from pruning"},
			&cli.BoolFlag{Name: "wait", Usage: "wait for async task completion"},
			&cli.DurationFlag{Name: "wait-timeout", Usage: "task wait timeout"},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "output format: table,json,yaml"},
		},
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			vmid, err := parseVMID(c.Args().First())
			if err != nil {
				return err
			}
			mode, err := pve.ParseBackupMode(c.String("mode"))
			if err != nil {
				return err
			}
			compress, err := pve.ParseBackupCompress(c.String("compress"))
			if err != nil {
				return err
			}
			// The API takes a 0/1 value; an unset flag means "leave default".
			protected := ""
			if c.IsSet("protected") {
				protected = "0"
				if c.Bool("protected") {
					protected = "1"
				}
			}

			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			result, err := pve.NewBackupService(rt.backend, rt.tasks).BackupGuest(c.Context, pve.BackupCreateOptions{
				Kind:          kind,
				VMID:          vmid,
				Node:          c.String("node"),
				Storage:       c.String("storage"),
				Mode:          mode,
				Compress:      compress,
				NotesTemplate: c.String("notes-template"),
				BwLimit:       c.Uint("bwlimit"),
				Protected:     protected,
			})
			if err != nil {
				return err
			}
			return output.WriteBackupResult(rt.stdout, rt.format, result)
		},
	}
}
