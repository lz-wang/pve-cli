package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/urfave/cli/v2"
	"golang.org/x/term"

	"github.com/lz-wang/pvectl/internal/config"
	"github.com/lz-wang/pvectl/internal/output"
)

func guideConfigInit(c *cli.Context, deps Dependencies) error {
	path, err := config.ExpandPath(c.String("config"))
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.App.ErrWriter, "No config file found at %s.\n", path); err != nil {
		return err
	}
	input := deps.withDefaults().Stdin
	file, isFile := input.(*os.File)
	if isFile && !term.IsTerminal(int(file.Fd())) {
		_, err := fmt.Fprintln(c.App.ErrWriter, "Run pve config view in a terminal for guided setup, or use pve config init --endpoint URL --token-id USER@REALM!TOKEN --token-secret SECRET (with the same --config path).")
		return err
	}
	prompt := configPrompter{reader: bufio.NewReader(input), out: c.App.ErrWriter}
	if isFile {
		prompt.terminal = file
	}
	if err := prompt.initialize(c, path); errors.Is(err, io.EOF) {
		_, err = fmt.Fprintln(c.App.ErrWriter, "\nInitialization cancelled; no config file was created.")
		return err
	} else {
		return err
	}
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
	nameDefault := c.String("profile")
	if nameDefault == "" {
		nameDefault = "home"
	}
	name, err := p.ask("Profile name", nameDefault, false, nil)
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
	answer, err = p.ask("Skip TLS certificate verification? (yes/no)", insecureDefault, false, validateConfigYesNo)
	if err != nil {
		return err
	}
	profile.InsecureSkipVerify = configAnswerYes(answer)
	if timeout := durationFlag(c, "timeout"); timeout > 0 {
		profile.Timeout = timeout.String()
	}
	profile.DefaultOutput = output.NormalizeFormat(stringFlag(c, "output"))
	if err := output.ValidateFormat(profile.DefaultOutput); err != nil {
		return err
	}
	cfg := config.Empty()
	if err := cfg.InitProfile(config.InitOptions{Name: name, Profile: profile, Use: true}); err != nil {
		return err
	}
	if err := config.SaveNew(path, cfg); errors.Is(err, os.ErrExist) {
		_, err := fmt.Fprintln(p.out, "A config file was created during setup; the existing file was kept. Run pve config view again to inspect it.")
		return err
	} else if err != nil {
		return err
	}
	_, err = fmt.Fprintf(p.out, "Config file created at %s.\n", path)
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
