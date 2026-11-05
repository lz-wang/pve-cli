package cmd

import (
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/urfave/cli/v2"
	"strings"
)

func backupRestorePlanCommand(deps Dependencies) *cli.Command {
	return &cli.Command{Name: "restore-plan", Usage: "Inspect an archive and a separate destination before transfer (read-only)", ArgsUsage: "ARCHIVE", Flags: append([]cli.Flag{
		&cli.StringFlag{Name: "from", Usage: "source profile (default: selected profile)"},
		&cli.StringFlag{Name: "to", Required: true, Usage: "destination profile"},
		&cli.StringFlag{Name: "node", Required: true, Usage: "source node"},
		&cli.StringFlag{Name: "target-node", Required: true},
		&cli.IntFlag{Name: "target-vmid", Required: true},
		&cli.StringFlag{Name: "type", Required: true, Usage: "vm or lxc"},
		&cli.StringFlag{Name: "storage", Usage: "destination disk storage"},
		&cli.StringSliceFlag{Name: "disk-storage", Usage: "planning-only disk=storage override"},
		&cli.StringSliceFlag{Name: "bridge-map", Usage: "source=target bridge mapping"},
	}, commonOutputFlags()...), Action: func(c *cli.Context) error {
		if err := requireNoExtraArgs(c, 1); err != nil {
			return err
		}
		if c.Args().Len() != 1 || c.Int("target-vmid") <= 0 {
			return fmt.Errorf("%w: archive and positive target-vmid required", pve.ErrInvalid)
		}
		kind := c.String("type")
		if kind != "vm" && kind != "lxc" {
			return fmt.Errorf("%w: type must be vm or lxc", pve.ErrInvalid)
		}
		disks, err := parseUniqueMapping(c.StringSlice("disk-storage"))
		if err != nil {
			return err
		}
		bridges, err := parseUniqueMapping(c.StringSlice("bridge-map"))
		if err != nil {
			return err
		}
		sides, err := buildCrossProfileSides(c, deps, crossProfileMigrationRequest{from: c.String("from"), to: c.String("to")})
		if err != nil {
			return err
		}
		format := crossProfileOutputFormat(c, sides)
		if err = output.ValidateFormat(format); err != nil {
			return err
		}
		plan, err := pve.PlanArchiveRestore(c.Context, sides.sourceBackend, sides.targetBackend, pve.ArchivePlanOptions{SourceNode: c.String("node"), RestoreOptions: pve.RestoreOptions{Kind: kind, Archive: c.Args().First(), Node: c.String("target-node"), VMID: c.Int("target-vmid"), Storage: c.String("storage"), BridgeMap: bridges}, DiskStorage: disks})
		if err != nil {
			return err
		}
		if err = output.WriteMigrationPlan(c.App.Writer, format, plan); err != nil {
			return err
		}
		if plan.Status == "failed" {
			return fmt.Errorf("%w: restore plan contains failed checks", pve.ErrConflict)
		}
		for _, r := range plan.Checks {
			if (r.Check == "archive_config" || r.Check == "storage" || r.Check == "network") && r.Status == "unknown" {
				return fmt.Errorf("restore plan inventory incomplete: %s", r.Check)
			}
		}
		return nil
	}}
}
func parseUniqueMapping(entries []string) (map[string]string, error) {
	result := map[string]string{}
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			return nil, fmt.Errorf("%w: mapping requires source=target", pve.ErrInvalid)
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("%w: duplicate mapping for %s", pve.ErrInvalid, key)
		}
		result[key] = value
	}
	return result, nil
}
