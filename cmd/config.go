package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pve-cli/v2/internal/config"
	"github.com/lz-wang/pve-cli/v2/internal/output"
)

func newConfigCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "Manage pve profiles",
		Subcommands: []*cli.Command{
			newConfigListCommand(deps),
			newConfigShowCommand(deps),
			newConfigAddCommand(deps),
			newConfigSetCommand(),
			newConfigUseCommand(),
		},
	}
}

func newConfigListCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "ls",
		Usage: "List profile names and endpoints",
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}
			cfg, err := config.Load(c.String("config"))
			if errors.Is(err, os.ErrNotExist) {
				return guideConfigInit(c, deps)
			}
			if err != nil {
				return fmt.Errorf("config error: %w", err)
			}
			rows := make([]output.ConfigProfileRow, 0, len(cfg.Profiles))
			for _, name := range configProfileNames(cfg) {
				rows = append(rows, output.ConfigProfileRow{Name: name, Endpoint: cfg.Profiles[name].Endpoint})
			}
			return output.WriteConfigProfileList(c.App.Writer, rows)
		},
	}
}

func newConfigShowCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "show",
		Usage:     "Show the current or named profile, or all profiles, as tables",
		ArgsUsage: "[NAME]",
		Flags:     []cli.Flag{&cli.BoolFlag{Name: "all", Usage: "show all profiles"}},
		Action: func(c *cli.Context) error {
			if c.NArg() > 1 {
				return errors.New("config show accepts at most one profile name")
			}
			name := c.Args().First()
			if name != "" && c.String("profile") != "" {
				return errors.New("choose either a profile name or --profile")
			}
			if name == "" {
				name = c.String("profile")
			}
			if c.Bool("all") && name != "" {
				return errors.New("--all cannot be combined with a profile name or --profile")
			}
			cfg, err := config.Load(c.String("config"))
			if errors.Is(err, os.ErrNotExist) {
				return guideConfigInit(c, deps)
			}
			if err != nil {
				return fmt.Errorf("config error: %w", err)
			}
			names := configProfileNames(cfg)
			if !c.Bool("all") {
				selected, _, err := cfg.SelectProfile(name)
				if err != nil {
					return fmt.Errorf("config error: %w", err)
				}
				names = []string{selected}
			}
			path, err := config.ExpandPath(c.String("config"))
			if err != nil {
				return err
			}
			path, err = filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("resolve config path: %w", err)
			}
			rows := make([]output.ConfigProfileRow, 0, len(names))
			for _, name := range names {
				profile := cfg.Profiles[name]
				rows = append(rows, output.ConfigProfileRow{
					Name: name, Current: name == cfg.CurrentProfile, Endpoint: profile.Endpoint,
					TokenID: profile.TokenID, TokenSecret: summarizeTokenSecret(profile.TokenSecret),
					TokenSecretEnv: profile.TokenSecretEnv, InsecureSkipVerify: profile.InsecureSkipVerify,
					Timeout: profile.Timeout, DefaultOutput: profile.DefaultOutput,
				})
			}
			if err := output.WriteConfigProfiles(c.App.Writer, rows); err != nil {
				return err
			}
			return writeConfigPath(c.App.Writer, path)
		},
	}
}

func newConfigSetCommand() *cli.Command {
	return &cli.Command{
		Name:      "set",
		Usage:     "Create or replace a profile",
		ArgsUsage: "NAME",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "endpoint", Usage: "PVE API endpoint, for example https://pve.lan:8006/api2/json"},
			&cli.StringFlag{Name: "token-id", Usage: "PVE API token id, for example automation@pve!pve"},
			&cli.StringFlag{Name: "token-secret", Usage: "PVE API token secret stored in plaintext; takes precedence over --token-secret-env"},
			&cli.StringFlag{Name: "token-secret-env", Usage: "environment variable containing the PVE API token secret; used when --token-secret is empty"},
			&cli.BoolFlag{Name: "insecure", Usage: "skip TLS certificate verification for this profile"},
			&cli.StringFlag{Name: "timeout", Value: "30s", Usage: "PVE API request timeout for this profile"},
			&cli.StringFlag{Name: "default-output", Value: output.FormatTable, Usage: "default output format: table,json,yaml"},
		},
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
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
			if err := cfg.SetProfile(c.Args().First(), config.Profile{
				Endpoint: c.String("endpoint"), TokenID: c.String("token-id"),
				TokenSecret: c.String("token-secret"), TokenSecretEnv: c.String("token-secret-env"),
				InsecureSkipVerify: c.Bool("insecure"), Timeout: c.String("timeout"), DefaultOutput: defaultOutput,
			}); err != nil {
				return fmt.Errorf("config error: %w", err)
			}
			return config.Save(c.String("config"), cfg)
		},
	}
}

func newConfigUseCommand() *cli.Command {
	return &cli.Command{
		Name:      "use",
		Usage:     "Select the current profile",
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
	}
}

func configProfileNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func summarizeTokenSecret(secret string) string {
	if secret == "" {
		return ""
	}
	chars := []rune(secret)
	if len(chars) <= 6 {
		return "*****"
	}
	return string(chars[:3]) + "*****" + string(chars[len(chars)-3:])
}

func writeConfigPath(w io.Writer, path string) error {
	displayPath := strings.NewReplacer(
		"\r", "\\r", "\n", "\\n", "\u0085", "\\u0085",
		"\u2028", "\\u2028", "\u2029", "\\u2029",
	).Replace(path)
	_, err := fmt.Fprintf(w, "Config file: %s\n", displayPath)
	return err
}
