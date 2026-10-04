package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultPath = "~/.config/pve/config.yaml"

type Config struct {
	CurrentProfile string             `yaml:"current_profile,omitempty"`
	Profiles       map[string]Profile `yaml:"profiles,omitempty"`
}

type Profile struct {
	Endpoint           string `yaml:"endpoint,omitempty"`
	TokenID            string `yaml:"token_id,omitempty"`
	TokenSecret        string `yaml:"token_secret,omitempty"`
	TokenSecretEnv     string `yaml:"token_secret_env,omitempty"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify,omitempty"`
	Timeout            string `yaml:"timeout,omitempty"`
	DefaultOutput      string `yaml:"default_output,omitempty"`
}

type InitOptions struct {
	Name      string
	Profile   Profile
	Overwrite bool
	Use       bool
}

func Empty() *Config {
	return &Config{Profiles: map[string]Profile{}}
}

func Load(path string) (*Config, error) {
	resolved, err := ExpandPath(path)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", resolved, err)
	}

	cfg := Empty()
	if len(data) == 0 {
		return cfg, nil
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", resolved, err)
	}
	cfg.ensure()
	return cfg, nil
}

func LoadOrEmpty(path string) (*Config, error) {
	cfg, err := Load(path)
	if err == nil {
		return cfg, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return Empty(), nil
	}
	return nil, err
}

func Save(path string, cfg *Config) error {
	resolved, err := ExpandPath(path)
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = Empty()
	}
	cfg.ensure()

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(resolved), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.Chmod(resolved, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("set config permissions %s: %w", resolved, err)
	}
	if err := os.WriteFile(resolved, data, 0o600); err != nil {
		return fmt.Errorf("write config %s: %w", resolved, err)
	}
	return nil
}

func ToYAML(cfg *Config) ([]byte, error) {
	if cfg == nil {
		cfg = Empty()
	}
	cfg.ensure()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return data, nil
}

func ExpandPath(path string) (string, error) {
	if path == "" {
		path = DefaultPath
	}
	path = os.ExpandEnv(path)

	if path == "~" || len(path) >= 2 && path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}

	return filepath.Clean(path), nil
}

func (c *Config) SetProfile(name string, profile Profile) error {
	if name == "" {
		return errors.New("profile name is required")
	}
	if err := validateProfile(profile); err != nil {
		return err
	}
	c.ensure()
	c.Profiles[name] = profile
	if c.CurrentProfile == "" {
		c.CurrentProfile = name
	}
	return nil
}

func (c *Config) InitProfile(options InitOptions) error {
	name := strings.TrimSpace(options.Name)
	if name == "" {
		return errors.New("profile name is required")
	}
	if err := validateProfile(options.Profile); err != nil {
		return err
	}

	c.ensure()
	if _, exists := c.Profiles[name]; exists && !options.Overwrite {
		return fmt.Errorf("profile %q already exists; use --overwrite to replace it", name)
	}
	c.Profiles[name] = options.Profile
	if options.Use {
		c.CurrentProfile = name
	}
	return nil
}

func (c *Config) UseProfile(name string) error {
	if name == "" {
		return errors.New("profile name is required")
	}
	c.ensure()
	if _, ok := c.Profiles[name]; !ok {
		return fmt.Errorf("profile %q not found", name)
	}
	c.CurrentProfile = name
	return nil
}

func (c *Config) SelectProfile(name string) (string, Profile, error) {
	c.ensure()
	if name == "" {
		name = c.CurrentProfile
	}
	if name == "" {
		return "", Profile{}, errors.New("profile is required; set current_profile or pass --profile")
	}
	profile, ok := c.Profiles[name]
	if !ok {
		return "", Profile{}, fmt.Errorf("profile %q not found", name)
	}
	return name, profile, nil
}

func ResolveTokenSecret(profile Profile) (string, error) {
	if profile.TokenSecret != "" {
		return profile.TokenSecret, nil
	}
	if profile.TokenSecretEnv == "" {
		return "", errors.New("token_secret or token_secret_env is required")
	}
	secret := os.Getenv(profile.TokenSecretEnv)
	if secret == "" {
		return "", fmt.Errorf("environment variable %s is empty", profile.TokenSecretEnv)
	}
	return secret, nil
}

func validateProfile(profile Profile) error {
	if profile.Endpoint == "" {
		return errors.New("endpoint is required")
	}
	if profile.TokenID == "" {
		return errors.New("token-id is required")
	}
	if profile.TokenSecret == "" && profile.TokenSecretEnv == "" {
		return errors.New("token-secret or token-secret-env is required")
	}
	return nil
}

func (c *Config) ensure() {
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
}
