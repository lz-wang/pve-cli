package cmd

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pve-cli/v2/internal/output"
	"github.com/lz-wang/pve-cli/v2/internal/pve"
)

// newVMCloudInitCommand manages PVE-native cloud-init settings on a VM.
func newVMCloudInitCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "cloud-init",
		Usage: "Inspect and manage PVE cloud-init settings",
		Subcommands: []*cli.Command{
			vmCloudInitGetCommand(deps),
			vmCloudInitSetCommand(deps),
			vmCloudInitRegenerateCommand(deps),
		},
	}
}

func vmCloudInitRegenerateCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "regenerate",
		Usage:     "Regenerate the cloud-init image so the next boot picks up pending changes",
		ArgsUsage: "VMID",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "node", Usage: "PVE node name"},
		},
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			vmid, err := parseVMID(c.Args().First())
			if err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			if err := pve.NewCloudInitService(rt.backend, rt.tasks, rt.logger, rt.verbose).Regenerate(c.Context, vmid, c.String("node")); err != nil {
				return err
			}
			fmt.Fprintln(rt.stdout, "cloud-init regenerated")
			return nil
		},
	}
}

func vmCloudInitGetCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "get",
		Usage:     "Show cloud-init settings",
		ArgsUsage: "VMID",
		Flags: append(
			[]cli.Flag{
				&cli.StringFlag{Name: "node", Usage: "PVE node name"},
			},
			commonOutputFlags()...,
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			vmid, err := parseVMID(c.Args().First())
			if err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			config, err := pve.NewCloudInitService(rt.backend, rt.tasks, rt.logger, rt.verbose).Get(c.Context, vmid, c.String("node"))
			if err != nil {
				return err
			}
			return output.WriteCloudInitConfig(rt.stdout, rt.format, config)
		},
	}
}

func vmCloudInitSetCommand(deps Dependencies) *cli.Command {
	flags := append(
		[]cli.Flag{
			&cli.StringFlag{Name: "node", Usage: "PVE node name"},
			&cli.StringFlag{Name: "user", Usage: "cloud-init user (ciuser)"},
			&cli.StringFlag{Name: "password-env", Usage: "environment variable holding the cloud-init password; never pass passwords as flags"},
			&cli.StringFlag{Name: "ssh-key-file", Usage: "file with public SSH keys (sshkeys)"},
			&cli.StringFlag{Name: "nameserver", Usage: "DNS server (nameserver)"},
			&cli.StringFlag{Name: "searchdomain", Usage: "DNS search domain (searchdomain)"},
			&cli.BoolFlag{Name: "wait", Usage: "wait for async task completion"},
			&cli.DurationFlag{Name: "wait-timeout", Usage: "task wait timeout"},
		},
		cloudInitIPConfigFlags()...,
	)

	return &cli.Command{
		Name:      "set",
		Usage:     "Update cloud-init settings",
		ArgsUsage: "VMID",
		Flags:     flags,
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			vmid, err := parseVMID(c.Args().First())
			if err != nil {
				return err
			}
			ipConfigs, err := parseCloudInitIPConfigs(c)
			if err != nil {
				return err
			}

			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			return pve.NewCloudInitService(rt.backend, rt.tasks, rt.logger, rt.verbose).Set(c.Context, vmid, c.String("node"), pve.CloudInitSetOptions{
				User:         c.String("user"),
				PasswordEnv:  c.String("password-env"),
				SSHKeyFile:   c.String("ssh-key-file"),
				IPConfigs:    ipConfigs,
				Nameserver:   c.String("nameserver"),
				SearchDomain: c.String("searchdomain"),
			})
		},
	}
}

func cloudInitIPConfigFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "ipconfig0", Usage: "ipconfig0 value, for example ip=dhcp"},
		&cli.StringFlag{Name: "ipconfig1", Usage: "ipconfig1 value"},
		&cli.StringFlag{Name: "ipconfig2", Usage: "ipconfig2 value"},
		&cli.StringFlag{Name: "ipconfig3", Usage: "ipconfig3 value"},
	}
}

func parseCloudInitIPConfigs(c *cli.Context) (map[string]string, error) {
	ipConfigs := make(map[string]string)
	for i := 0; i < 4; i++ {
		name := fmt.Sprintf("ipconfig%d", i)
		if flagIsSet(c, name) {
			ipConfigs[name] = c.String(name)
		}
	}
	return ipConfigs, nil
}
