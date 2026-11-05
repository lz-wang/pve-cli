package cmd

import (
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/lz-wang/pve-cli/v3/internal/ssh"
	"github.com/urfave/cli/v2"
	"regexp"
	"strings"
)

func newConfigSSHCommand() *cli.Command {
	flags := []cli.Flag{&cli.StringFlag{Name: "host", Usage: "SSH hostname or address"}, &cli.StringFlag{Name: "node", Usage: "PVE node served by this SSH host"}, &cli.StringFlag{Name: "user", Usage: "SSH user (default: root)"}, &cli.IntFlag{Name: "port", Usage: "SSH port (default: 22)"}, &cli.StringFlag{Name: "identity-file", Usage: "existing private key path; empty clears the override"}, &cli.StringFlag{Name: "known-hosts", Usage: "known_hosts path; empty uses OpenSSH defaults"}}
	return &cli.Command{Name: "ssh", Usage: "Manage optional SSH settings without replacing API credentials", Subcommands: []*cli.Command{
		{Name: "set", Usage: "Set or update SSH fields on an existing profile", ArgsUsage: "NAME", Flags: flags, Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			cfg, err := config.Load(c.String("config"))
			if err != nil {
				return err
			}
			name, p, err := cfg.SelectProfile(c.Args().First())
			if err != nil {
				return err
			}
			changed := false
			for _, f := range flags {
				if c.IsSet(f.Names()[0]) {
					changed = true
				}
			}
			if !changed {
				return fmt.Errorf("%w: specify at least one SSH field", pve.ErrInvalid)
			}
			settings := ssh.Profile{}
			if p.SSH != nil {
				settings = *p.SSH
			}
			for flag, dest := range map[string]*string{"host": &settings.Host, "node": &settings.Node, "user": &settings.User, "identity-file": &settings.IdentityFile, "known-hosts": &settings.KnownHosts} {
				if c.IsSet(flag) {
					*dest = strings.TrimSpace(c.String(flag))
				}
			}
			if c.IsSet("port") {
				settings.Port = c.Int("port")
				if settings.Port < 1 || settings.Port > 65535 {
					return fmt.Errorf("%w: SSH port must be 1..65535", pve.ErrInvalid)
				}
			}
			if !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:-]*$`).MatchString(settings.Host) {
				return fmt.Errorf("%w: invalid SSH host", pve.ErrInvalid)
			}
			if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`).MatchString(settings.Node) {
				return fmt.Errorf("%w: explicit SSH node binding is required", pve.ErrInvalid)
			}
			if settings.User != "" && !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`).MatchString(settings.User) {
				return fmt.Errorf("%w: invalid SSH user", pve.ErrInvalid)
			}
			if err = settings.Validate(); err != nil {
				return err
			}
			p.SSH = &settings
			if err = cfg.SetProfile(name, p); err != nil {
				return err
			}
			return config.Save(c.String("config"), cfg)
		}},
		{Name: "clear", Usage: "Remove the local SSH section from an existing profile", ArgsUsage: "NAME", Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			cfg, err := config.Load(c.String("config"))
			if err != nil {
				return err
			}
			name, p, err := cfg.SelectProfile(c.Args().First())
			if err != nil {
				return err
			}
			p.SSH = nil
			if err = cfg.SetProfile(name, p); err != nil {
				return err
			}
			return config.Save(c.String("config"), cfg)
		}},
	}}
}
