package cmd

import (
	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pve-cli/v2/internal/output"
	"github.com/lz-wang/pve-cli/v2/internal/pve"
)

func newStorageCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "storage",
		Usage: "Inspect Proxmox VE storages",
		Subcommands: []*cli.Command{
			storageListCommand(deps),
			storageUsageCommand(deps),
			storageGetCommand(deps),
			storageContentCommand(deps),
		},
	}
}

func storageUsageCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "usage",
		Usage: "Show storage usage across nodes",
		Flags: append(
			[]cli.Flag{
				&cli.StringFlag{Name: "node", Usage: "PVE node name"},
				&cli.StringFlag{Name: "content", Usage: "filter by content type, for example backup or iso"},
				&cli.StringFlag{Name: "type", Usage: "filter by storage type, for example dir or lvmthin"},
				&cli.BoolFlag{Name: "active", Usage: "show only active storages"},
				&cli.BoolFlag{Name: "enabled", Usage: "show only enabled storages"},
			},
			commonOutputFlags()...,
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			rows, err := pve.NewStorageService(rt.backend).List(c.Context, pve.StorageListOptions{
				Node:    c.String("node"),
				Content: c.String("content"),
				Type:    c.String("type"),
				Active:  c.Bool("active"),
				Enabled: c.Bool("enabled"),
			})
			if err != nil {
				return err
			}
			return output.WriteStorageUsageRows(rt.stdout, rt.format, rows)
		},
	}
}

func storageListCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "ls",
		Usage: "List storage status",
		Flags: append(
			[]cli.Flag{
				&cli.StringFlag{Name: "node", Usage: "PVE node name"},
				&cli.StringFlag{Name: "content", Usage: "filter by content type, for example backup or iso"},
				&cli.StringFlag{Name: "type", Usage: "filter by storage type, for example dir or lvmthin"},
				&cli.BoolFlag{Name: "active", Usage: "show only active storages"},
				&cli.BoolFlag{Name: "enabled", Usage: "show only enabled storages"},
			},
			commonOutputFlags()...,
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			rows, err := pve.NewStorageService(rt.backend).List(c.Context, pve.StorageListOptions{
				Node:    c.String("node"),
				Content: c.String("content"),
				Type:    c.String("type"),
				Active:  c.Bool("active"),
				Enabled: c.Bool("enabled"),
			})
			if err != nil {
				return err
			}
			return output.WriteStorageRows(rt.stdout, rt.format, rows)
		},
	}
}

func storageGetCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "get",
		Usage:     "Show storage status on a node",
		ArgsUsage: "STORAGE",
		Flags: append(
			[]cli.Flag{
				&cli.StringFlag{Name: "node", Usage: "PVE node name", Required: true},
			},
			commonOutputFlags()...,
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			row, err := pve.NewStorageService(rt.backend).Get(c.Context, c.String("node"), c.Args().First())
			if err != nil {
				return err
			}
			return output.WriteStorageDetail(rt.stdout, rt.format, row)
		},
	}
}

func storageContentCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "content",
		Usage: "Inspect storage content",
		Subcommands: []*cli.Command{
			storageContentListCommand(deps),
		},
	}
}

func storageContentListCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "ls",
		Usage: "List content on a storage",
		Flags: append(
			[]cli.Flag{
				&cli.StringFlag{Name: "node", Usage: "PVE node name", Required: true},
				&cli.StringFlag{Name: "storage", Usage: "storage name", Required: true},
				&cli.StringFlag{Name: "content", Usage: "filter by content type, for example backup or iso"},
				&cli.IntFlag{Name: "vmid", Usage: "filter by VMID/CTID"},
			},
			commonOutputFlags()...,
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			rows, err := pve.NewStorageService(rt.backend).ListContent(c.Context, pve.StorageContentListOptions{
				Node:    c.String("node"),
				Storage: c.String("storage"),
				Content: c.String("content"),
				VMID:    c.Int("vmid"),
			})
			if err != nil {
				return err
			}
			return output.WriteStorageContentRows(rt.stdout, rt.format, rows)
		},
	}
}
