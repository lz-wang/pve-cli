package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v2"
	"golang.org/x/term"

	"github.com/lz-wang/pvectl/internal/config"
	"github.com/lz-wang/pvectl/internal/output"
)

func newConfigAddCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:  "add",
		Usage: "Interactively add a profile",
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}
			path, err := configInteractivePath(c)
			if err != nil {
				return err
			}
			cfg, err := config.Load(path)
			missing := errors.Is(err, os.ErrNotExist)
			if missing {
				cfg = config.Empty()
			} else if err != nil {
				return fmt.Errorf("config error: %w", err)
			}
			prompt, interactive := newConfigPrompter(c, deps)
			if !interactive {
				return errors.New("config add requires a terminal; use pve config update NAME --endpoint URL --token-id USER@REALM!TOKEN --token-secret SECRET for noninteractive setup (with the same --config path)")
			}
			if err := prompt.addProfile(c, path, cfg, missing, true); errors.Is(err, io.EOF) {
				_, err = fmt.Fprintln(c.App.ErrWriter, "\nProfile addition cancelled; no config file was changed.")
				return err
			} else {
				return err
			}
		},
	}
}

func guideConfigInit(c *cli.Context, deps Dependencies) error {
	path, err := configInteractivePath(c)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.App.ErrWriter, "No config file found at %s.\n", path); err != nil {
		return err
	}
	prompt, interactive := newConfigPrompter(c, deps)
	if !interactive {
		_, err := fmt.Fprintln(c.App.ErrWriter, "Run pve config add in a terminal for guided setup, or use pve config update NAME --endpoint URL --token-id USER@REALM!TOKEN --token-secret SECRET (with the same --config path).")
		return err
	}
	if err := prompt.initialize(c, path); errors.Is(err, io.EOF) {
		_, err = fmt.Fprintln(c.App.ErrWriter, "\nInitialization cancelled; no config file was created.")
		return err
	} else {
		return err
	}
}

func configInteractivePath(c *cli.Context) (string, error) {
	path, err := config.ExpandPath(c.String("config"))
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve config path: %w", err)
	}
	return path, nil
}

func newConfigPrompter(c *cli.Context, deps Dependencies) (configPrompter, bool) {
	input := deps.withDefaults().Stdin
	prompt := configPrompter{reader: bufio.NewReader(input), out: c.App.ErrWriter}
	if file, isFile := input.(*os.File); isFile {
		if !term.IsTerminal(int(file.Fd())) {
			return prompt, false
		}
		prompt.terminal = file
	}
	return prompt, true
}

type configPrompter struct {
	reader   *bufio.Reader
	out      io.Writer
	terminal *os.File
}

func (p *configPrompter) initialize(c *cli.Context, path string) error {
	answer, err := p.ask("Initialize a config file now? (yes/no)", "yes", false, validateConfigYesNo)
	if err != nil {
		return err
	}
	if !configAnswerYes(answer) {
		_, err := fmt.Fprintln(p.out, "Initialization skipped; no config file was created.")
		return err
	}
	return p.addProfile(c, path, config.Empty(), true, false)
}

func (p *configPrompter) addProfile(c *cli.Context, path string, cfg *config.Config, missing, askCurrent bool) error {
	nameDefault := c.String("profile")
	if nameDefault == "" {
		nameDefault = "home"
	}
	name, err := p.ask("Profile name", nameDefault, false, func(value string) error {
		if _, exists := cfg.Profiles[value]; exists {
			return fmt.Errorf("profile %q already exists; choose another name", value)
		}
		return nil
	})
	if err != nil {
		return err
	}
	endpoint, err := p.ask("PVE API endpoint (e.g. https://pve.lan:8006/api2/json)", "", false, validateConfigEndpoint)
	if err != nil {
		return err
	}
	tokenID, err := p.ask("Token ID (USER@REALM!TOKEN)", "", false, nil)
	if err != nil {
		return err
	}
	source, err := p.ask("Token source (token/env)", "token", false, func(value string) error {
		if value != "token" && value != "env" {
			return errors.New("enter token for plaintext storage or env for an environment reference")
		}
		return nil
	})
	if err != nil {
		return err
	}
	profile := config.Profile{Endpoint: endpoint, TokenID: tokenID, Timeout: "30s", DefaultOutput: output.FormatTable}
	if source == "token" {
		profile.TokenSecret, err = p.ask("Token secret (stored in plaintext)", "", true, nil)
	} else {
		profile.TokenSecretEnv, err = p.ask("Token-secret environment variable name", "", false, nil)
	}
	if err != nil {
		return err
	}
	insecureDefault := "no"
	if boolFlag(c, "insecure") {
		insecureDefault = "yes"
	}
	answer, err := p.ask("Skip TLS certificate verification? (yes/no)", insecureDefault, false, validateConfigYesNo)
	if err != nil {
		return err
	}
	profile.InsecureSkipVerify = configAnswerYes(answer)
	if timeout := durationFlag(c, "api-timeout"); timeout > 0 {
		profile.Timeout = timeout.String()
	}
	use := true
	if askCurrent {
		useDefault := "no"
		if cfg.CurrentProfile == "" {
			useDefault = "yes"
		}
		answer, err = p.ask("Set as current profile? (yes/no)", useDefault, false, validateConfigYesNo)
		if err != nil {
			return err
		}
		use = configAnswerYes(answer)
	}
	if !missing {
		cfg, err = config.Load(path)
		if err != nil {
			return fmt.Errorf("reload config before adding profile: %w", err)
		}
		if _, exists := cfg.Profiles[name]; exists {
			return fmt.Errorf("profile %q was created during setup; the existing profile was kept", name)
		}
	}
	if err := cfg.InitProfile(config.InitOptions{Name: name, Profile: profile, Use: use}); err != nil {
		return err
	}
	if missing {
		if err := config.SaveNew(path, cfg); errors.Is(err, os.ErrExist) {
			_, err := fmt.Fprintln(p.out, "A config file was created during setup; the existing file was kept. Run pve config show --all again to inspect it.")
			return err
		} else if err != nil {
			return err
		}
		_, err = fmt.Fprintf(p.out, "Config file created at %s.\n", path)
		return err
	}
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	_, err = fmt.Fprintf(p.out, "Profile %q added to %s.\n", name, path)
	return err
}

func (p *configPrompter) ask(label, defaultValue string, secret bool, validate func(string) error) (string, error) {
	for {
		prompt := label
		if defaultValue != "" {
			prompt += " [" + defaultValue + "]"
		}
		if _, err := fmt.Fprint(p.out, prompt+": "); err != nil {
			return "", err
		}
		var value string
		if secret && p.terminal != nil {
			data, err := term.ReadPassword(int(p.terminal.Fd()))
			if _, printErr := fmt.Fprintln(p.out); printErr != nil {
				return "", printErr
			}
			if err != nil {
				return "", err
			}
			value = string(data)
		} else {
			line, err := p.reader.ReadString('\n')
			if err != nil && (!errors.Is(err, io.EOF) || line == "") {
				return "", err
			}
			value = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		}
		if !secret {
			value = strings.TrimSpace(value)
		}
		if value == "" {
			value = defaultValue
		}
		var err error
		if value == "" {
			err = errors.New("a value is required")
		} else if validate != nil {
			err = validate(value)
		}
		if err != nil {
			if _, err := fmt.Fprintln(p.out, err.Error()); err != nil {
				return "", err
			}
			continue
		}
		return value, nil
	}
}

func validateConfigEndpoint(value string) error {
	endpoint, err := url.Parse(value)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return errors.New("enter an http:// or https:// API URL with a host")
	}
	return nil
}

func validateConfigYesNo(value string) error {
	switch strings.ToLower(value) {
	case "y", "yes", "n", "no":
		return nil
	default:
		return errors.New("enter yes or no")
	}
}

func configAnswerYes(value string) bool {
	return strings.EqualFold(value, "y") || strings.EqualFold(value, "yes")
}
