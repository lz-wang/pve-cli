package cmd

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
)

func newCheckCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "check",
		Usage: "Check HomeLab health (nodes, storage usage, optional backup coverage)",
		Flags: append(
			[]cli.Flag{
				&cli.IntFlag{Name: "cpu-warn", Value: 85, Usage: "CPU warn percent"},
				&cli.IntFlag{Name: "memory-warn", Value: 90, Usage: "memory warn percent"},
				&cli.StringFlag{Name: "node", Usage: "limit checks to one PVE node"},
				&cli.IntFlag{Name: "storage-warn", Value: pve.DefaultStorageWarnPercent, Usage: "storage usage warn threshold in percent"},
				&cli.IntFlag{Name: "storage-fail", Value: pve.DefaultStorageFailPercent, Usage: "storage usage fail threshold in percent"},
				&cli.StringFlag{Name: "backup-tag", Usage: "check backup coverage for guests with this tag"},
				&cli.DurationFlag{Name: "backup-max-age", Usage: "maximum accepted backup age for tagged guests, for example 36h"},
				&cli.BoolFlag{Name: "strict", Usage: "treat warnings as failures for the exit code"},
			},
			commonOutputFlags()...,
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}
			for _, name := range []string{"cpu-warn", "memory-warn", "storage-warn", "storage-fail"} {
				if c.Int(name) < 1 || c.Int(name) > 100 {
					return fmt.Errorf("invalid %s: expected 1..100", name)
				}
			}
			warn := c.Int("storage-warn")
			fail := c.Int("storage-fail")
			if warn <= 0 || fail <= 0 {
				return fmt.Errorf("storage thresholds must be positive")
			}
			if warn >= fail {
				return fmt.Errorf("storage-warn %d must be lower than storage-fail %d", warn, fail)
			}
			backupTag := c.String("backup-tag")
			backupMaxAge := durationFlag(c, "backup-max-age")
			if (backupTag == "") != (backupMaxAge <= 0) {
				return fmt.Errorf("--backup-tag and --backup-max-age must be used together")
			}

			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			result, err := pve.NewCheckService(rt.backend).Run(c.Context, pve.CheckOptions{
				CPUWarn: c.Int("cpu-warn"), MemoryWarn: c.Int("memory-warn"),
				Node:         c.String("node"),
				StorageWarn:  warn,
				StorageFail:  fail,
				BackupTag:    backupTag,
				BackupMaxAge: backupMaxAge,
			})
			if err != nil {
				return err
			}
			if err := output.WriteCheckRows(rt.stdout, rt.format, result.Rows); err != nil {
				return err
			}
			if result.Failed {
				return fmt.Errorf("health check failed")
			}
			if result.Warned && c.Bool("strict") {
				return fmt.Errorf("health check reported warnings in strict mode")
			}
			return nil
		},
	}
}
