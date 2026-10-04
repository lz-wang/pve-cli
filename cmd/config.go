package cmd

import (
	"fmt"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/config"
	"github.com/lz-wang/pvectl/internal/output"
)

func newConfigCommand() *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "Manage pve config",
		Subcommands: []*cli.Command{
			{
				Name:  "init",
				Usage: "Initialize a default HomeLab profile",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "name", Value: "home", Usage: "profile name"},
					&cli.StringFlag{Name: "endpoint", Usage: "PVE API endpoint, for example https://pve.lan:8006/api2/json", Required: true},
					&cli.StringFlag{Name: "token-id", Usage: "PVE API token id, for example automation@pve!pve", Required: true},
					&cli.StringFlag{Name: "token-secret-env", Usage: "environment variable containing the PVE API token secret", Required: true},
					&cli.BoolFlag{Name: "insecure", Usage: "skip TLS certificate verification for this profile"},
					&cli.StringFlag{Name: "timeout", Value: "30s", Usage: "PVE API request timeout for this profile"},
					&cli.StringFlag{Name: "default-output", Value: output.FormatTable, Usage: "default output format: table,json,yaml"},
					&cli.BoolFlag{Name: "overwrite", Usage: "overwrite an existing profile"},
					&cli.BoolFlag{Name: "no-use", Usage: "do not set the initialized profile as current"},
				},
				Action: func(c *cli.Context) error {
					if err := requireNoExtraArgs(c, 0); err != nil {
						return err
					}
					defaultOutput := output.NormalizeFormat(c.String("default-output"))
					if err := output.ValidateFormat(defaultOutput); err != nil {
						return err
					}
					if _, err := time.ParseDuration(c.String("timeout")); err != nil {
						return fmt.Errorf("invalid timeout %q: %w", c.String("timeout"), err)
					}

					cfg, err := config.LoadOrEmpty(c.String("config"))
					if err != nil {
						return fmt.Errorf("config error: %w", err)
					}
					if err := cfg.InitProfile(config.InitOptions{
						Name: c.String("name"),
						Profile: config.Profile{
							Endpoint:           c.String("endpoint"),
							TokenID:            c.String("token-id"),
							TokenSecretEnv:     c.String("token-secret-env"),
							InsecureSkipVerify: c.Bool("insecure"),
							Timeout:            c.String("timeout"),
							DefaultOutput:      defaultOutput,
						},
						Overwrite: c.Bool("overwrite"),
						Use:       !c.Bool("no-use"),
					}); err != nil {
						return fmt.Errorf("config error: %w", err)
					}
					return config.Save(c.String("config"), cfg)
				},
			},
			{
				Name:      "set-profile",
				Usage:     "Create or update a profile",
				ArgsUsage: "NAME",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "endpoint", Usage: "PVE API endpoint, for example https://pve.lan:8006/api2/json", Required: true},
					&cli.StringFlag{Name: "token-id", Usage: "PVE API token id, for example automation@pve!pve", Required: true},
					&cli.StringFlag{Name: "token-secret-env", Usage: "environment variable containing the PVE API token secret", Required: true},
					&cli.BoolFlag{Name: "insecure", Usage: "skip TLS certificate verification for this profile"},
					&cli.StringFlag{Name: "timeout", Usage: "PVE API request timeout for this profile"},
					&cli.StringFlag{Name: "default-output", Usage: "default output format: table,json,yaml"},
				},
				Action: func(c *cli.Context) error {
					if err := requireNoExtraArgs(c, 1); err != nil {
						return err
					}
					defaultOutput := c.String("default-output")
					if defaultOutput != "" {
						if err := output.ValidateFormat(defaultOutput); err != nil {
							return err
						}
					}

					cfg, err := config.LoadOrEmpty(c.String("config"))
					if err != nil {
						return fmt.Errorf("config error: %w", err)
					}
					if err := cfg.SetProfile(c.Args().First(), config.Profile{
						Endpoint:           c.String("endpoint"),
						TokenID:            c.String("token-id"),
						TokenSecretEnv:     c.String("token-secret-env"),
						InsecureSkipVerify: c.Bool("insecure"),
						Timeout:            c.String("timeout"),
						DefaultOutput:      defaultOutput,
					}); err != nil {
						return fmt.Errorf("config error: %w", err)
					}
					return config.Save(c.String("config"), cfg)
				},
			},
			{
				Name:      "use-profile",
				Usage:     "Set the current profile",
				ArgsUsage: "NAME",
				Action: func(c *cli.Context) error {
					if err := requireNoExtraArgs(c, 1); err != nil {
						return err
					}
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return fmt.Errorf("config error: %w", err)
					}
					if err := cfg.UseProfile(c.Args().First()); err != nil {
						return fmt.Errorf("config error: %w", err)
					}
					return config.Save(c.String("config"), cfg)
				},
			},
			{
				Name:  "current-profile",
				Usage: "Print the current profile",
				Action: func(c *cli.Context) error {
					if err := requireNoExtraArgs(c, 0); err != nil {
						return err
					}
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return fmt.Errorf("config error: %w", err)
					}
					if cfg.CurrentProfile == "" {
						return fmt.Errorf("config error: current_profile is empty")
					}
					_, err = fmt.Fprintln(c.App.Writer, cfg.CurrentProfile)
					return err
				},
			},
			{
				Name:  "view",
				Usage: "Print the config file",
				Action: func(c *cli.Context) error {
					if err := requireNoExtraArgs(c, 0); err != nil {
						return err
					}
					cfg, err := config.Load(c.String("config"))
					if err != nil {
						return fmt.Errorf("config error: %w", err)
					}
					data, err := config.ToYAML(cfg)
					if err != nil {
						return err
					}
					_, err = c.App.Writer.Write(data)
					return err
				},
			},
			removedContextCommand("set-context", "set-profile"),
			removedContextCommand("use-context", "use-profile"),
			removedContextCommand("current-context", "current-profile"),
		},
	}
}

func removedContextCommand(name, replacement string) *cli.Command {
	return &cli.Command{
		Name:   name,
		Hidden: true,
		Action: func(*cli.Context) error {
			return fmt.Errorf("config %s was removed; use config %s", name, replacement)
		},
	}
}
