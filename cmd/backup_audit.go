package cmd

import (
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/urfave/cli/v2"
	"time"
)

func backupAuditCommand(deps Dependencies) *cli.Command {
	return &cli.Command{Name: "audit", Usage: "Audit guest backup freshness and current configuration exclusions (read-only)", Flags: append([]cli.Flag{
		&cli.StringFlag{Name: "node"},
		&cli.StringFlag{Name: "type", Value: "all", Usage: "all,vm,lxc"},
		&cli.IntSliceFlag{Name: "vmid", Usage: "selected guest IDs, repeatable"},
		&cli.StringSliceFlag{Name: "tag", Usage: "require all selected tags"},
		&cli.BoolFlag{Name: "include-templates", Usage: "include guest templates"},
		&cli.DurationFlag{Name: "max-age", Value: 7 * 24 * time.Hour, Usage: "maximum archive age"},
		&cli.BoolFlag{Name: "strict", Usage: "exit nonzero unless selected guests have recent archives without configuration exclusions"},
	}, commonOutputFlags()...), Action: func(c *cli.Context) error {
		if err := requireNoExtraArgs(c, 0); err != nil {
			return err
		}
		kind, err := pve.ParseGuestListType(c.String("type"))
		if err != nil {
			return err
		}
		if c.Duration("max-age") <= 0 {
			return fmt.Errorf("%w: max-age must be positive", pve.ErrInvalid)
		}
		for _, id := range c.IntSlice("vmid") {
			if id <= 0 {
				return fmt.Errorf("%w: vmid must be positive", pve.ErrInvalid)
			}
		}
		rt, err := buildRuntime(c, deps)
		if err != nil {
			return err
		}
		report, err := pve.AuditBackups(c.Context, rt.backend, pve.BackupAuditOptions{Node: c.String("node"), Kind: kind, IDs: c.IntSlice("vmid"), Tags: c.StringSlice("tag"), IncludeTemplates: c.Bool("include-templates"), MaxAge: c.Duration("max-age")})
		if err != nil {
			return err
		}
		if err = output.WriteBackupAudit(rt.stdout, rt.format, report); err != nil {
			return err
		}
		if !report.Complete {
			return fmt.Errorf("backup audit incomplete")
		}
		if c.Bool("strict") {
			if len(report.Rows) == 0 {
				return fmt.Errorf("backup audit selected no guests")
			}
			for _, r := range report.Rows {
				if r.Status != "recent_candidate" || len(r.Findings) > 2 {
					return fmt.Errorf("backup audit found missing, stale or excluded data")
				}
			}
		}
		return nil
	}}
}
