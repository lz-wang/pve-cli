package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lz-wang/pvectl/internal/config"
	"github.com/lz-wang/pvectl/internal/pve"
)

func TestConfigShowGuidedInitialization(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		flags    []string
		profile  string
		secret   string
		env      string
		insecure bool
		timeout  string
		output   string
	}{
		{
			name: "plaintext defaults", input: "\n\nhttps://pve.example:8006/api2/json\nautomation@pve!test\n\nfake-interactive-token\n\n",
			profile: "home", secret: "fake-interactive-token", timeout: "30s", output: "table",
		},
		{
			name: "environment and global defaults", input: "yes\n\nhttps://pve.example:8006/api2/json\nautomation@pve!test\nenv\nPVE_INTERACTIVE_TOKEN\n\n",
			flags:   []string{"--profile", "lab", "--api-timeout", "1m", "--insecure"},
			profile: "lab", env: "PVE_INTERACTIVE_TOKEN", insecure: true, timeout: "1m0s", output: "table",
		},
		{
			name: "invalid responses are retried", input: "maybe\ny\ncustom\n\nftp://pve.example\nhttps://pve.example:8006/api2/json\n\nautomation@pve!test\nwrong\ntoken\n\nfake-retry-token\nwrong\nyes\n",
			profile: "custom", secret: "fake-retry-token", insecure: true, timeout: "30s", output: "table",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nested", "config.yaml")
			args := append([]string{"pve", "--config", path}, tc.flags...)
			args = append(args, "config", "show")
			var stdout, stderr bytes.Buffer
			err := RunWithDependencies(args, "test", Dependencies{
				Stdin: strings.NewReader(tc.input), Stdout: &stdout, Stderr: &stderr,
				BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) {
					t.Fatal("initialization must not contact Proxmox")
					return nil, nil
				},
			})
			if err != nil {
				t.Fatalf("show: %v", err)
			}
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatalf("load initialized config: %v", err)
			}
			profile := cfg.Profiles[tc.profile]
			if cfg.CurrentProfile != tc.profile || profile.Endpoint != "https://pve.example:8006/api2/json" || profile.TokenID != "automation@pve!test" {
				t.Fatal("guided initialization did not save the requested profile")
			}
			if profile.TokenSecret != tc.secret || profile.TokenSecretEnv != tc.env || profile.InsecureSkipVerify != tc.insecure || profile.Timeout != tc.timeout || profile.DefaultOutput != tc.output {
				t.Fatal("guided initialization did not save the requested profile options")
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), "No config file found") || !strings.Contains(stderr.String(), "Config file created at "+path) {
				t.Fatal("setup messages should go to stderr and leave stdout empty")
			}
			if tc.secret != "" && strings.Contains(stderr.String(), tc.secret) {
				t.Fatal("setup output must not contain the token value")
			}
			stdout.Reset()
			stderr.Reset()
			if err := RunWithDependencies([]string{"pve", "--config", path, "config", "show"}, "test", Dependencies{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr}); err != nil {
				t.Fatalf("show saved config: %v", err)
			}
			if !strings.Contains(stdout.String(), tc.profile) || !strings.Contains(stdout.String(), profile.Endpoint) || !strings.HasSuffix(stdout.String(), "Config file: "+path+"\n") || stderr.Len() != 0 {
				t.Fatal("existing config should print the profile table and config path without prompts")
			}
			if tc.secret != "" && strings.Contains(stdout.String(), tc.secret) {
				t.Fatal("existing config output must summarize plaintext tokens")
			}
		})
	}
}

func TestConfigShowInitializationCanBeCancelled(t *testing.T) {
	for _, input := range []string{"no\n", "", "yes\nhome\n", "yes\nhome\nhttps://pve.example:8006/api2/json\nautomation@pve!test\ntoken\nfake-unsaved-token\n"} {
		t.Run(strings.ReplaceAll(input, "\n", "_"), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			var stdout, stderr bytes.Buffer
			err := RunWithDependencies([]string{"pve", "--config", path, "config", "show"}, "test", Dependencies{Stdin: strings.NewReader(input), Stdout: &stdout, Stderr: &stderr})
			if err != nil {
				t.Fatalf("cancelled setup should succeed: %v", err)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("cancelled setup must not create a file")
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), "no config file was created") {
				t.Fatal("cancelled setup should report its status on stderr")
			}
		})
	}
}

func TestConfigShowMissingConfigWithNonterminalInput(t *testing.T) {
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := input.Close(); err != nil {
			t.Error(err)
		}
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
	}()
	path := filepath.Join(t.TempDir(), "config.yaml")
	var stdout, stderr bytes.Buffer
	err = RunWithDependencies([]string{"pve", "--config", path, "config", "show"}, "test", Dependencies{Stdin: input, Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		t.Fatalf("noninteractive show should succeed: %v", err)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "No config file found") || !strings.Contains(stderr.String(), "pve config set NAME --endpoint") || strings.Contains(stderr.String(), "Initialize a config file now?") {
		t.Fatal("nonterminal input should receive a setup hint without waiting for input")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("noninteractive show must not create a file")
	}
}

func TestConfigShowKeepsExistingFileCreatedDuringSetup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := "current_profile: other\nprofiles: {}\n"
	var stdout, stderr bytes.Buffer
	created := false
	writer := configShowTestWriter(func(data []byte) (int, error) {
		if !created && strings.Contains(string(data), "Token secret (stored in plaintext)") {
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				return 0, err
			}
			created = true
		}
		return stderr.Write(data)
	})
	err := RunWithDependencies([]string{"pve", "--config", path, "config", "show"}, "test", Dependencies{
		Stdin:  strings.NewReader("yes\nhome\nhttps://pve.example:8006/api2/json\nautomation@pve!test\ntoken\nfake-race-token\nno\n"),
		Stdout: &stdout, Stderr: writer,
	})
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original || !strings.Contains(stderr.String(), "existing file was kept") {
		t.Fatal("setup must not overwrite a file created while prompting")
	}
}

func TestConfigShowStillReportsInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("profiles: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	err := RunWithDependencies([]string{"pve", "--config", path, "config", "show"}, "test", Dependencies{Stdin: strings.NewReader("yes\n"), Stdout: io.Discard, Stderr: &stderr})
	if err == nil || !strings.Contains(err.Error(), "parse config") || stderr.Len() != 0 {
		t.Fatal("only a missing config file should trigger setup")
	}
}

type configShowTestWriter func([]byte) (int, error)

func (w configShowTestWriter) Write(data []byte) (int, error) { return w(data) }
