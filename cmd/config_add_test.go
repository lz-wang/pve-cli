package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lz-wang/pvectl/internal/config"
)

func TestConfigAddProfile(t *testing.T) {
	for _, tc := range []struct {
		name        string
		existing    bool
		current     string
		input       string
		wantCurrent string
	}{
		{name: "new config defaults current", input: "\n", wantCurrent: "lab"},
		{name: "new config without current", input: "no\n"},
		{name: "existing preserves current", existing: true, current: "home", input: "\n", wantCurrent: "home"},
		{name: "existing chooses current", existing: true, current: "home", input: "yes\n", wantCurrent: "lab"},
		{name: "empty current defaults yes", existing: true, input: "\n", wantCurrent: "lab"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			original := config.Profile{Endpoint: "https://home.example:8006/api2/json", TokenID: "automation@pve!home", TokenSecretEnv: "PVE_TEST_TOKEN"}
			if tc.existing {
				if err := config.Save(path, &config.Config{CurrentProfile: tc.current, Profiles: map[string]config.Profile{"home": original}}); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr bytes.Buffer
			input := "\nhttps://lab.example:8006/api2/json\nautomation@pve!lab\ntoken\nfake-add-token\nno\n" + tc.input
			err := RunWithDependencies([]string{"pve", "--config", path, "--profile", "lab", "--api-timeout", "1m", "config", "add"}, "test", Dependencies{
				Stdin: strings.NewReader(input), Stdout: &stdout, Stderr: &stderr,
			})
			if err != nil {
				t.Fatalf("add: %v", err)
			}
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			profile := cfg.Profiles["lab"]
			if cfg.CurrentProfile != tc.wantCurrent || profile.Endpoint != "https://lab.example:8006/api2/json" || profile.TokenID != "automation@pve!lab" || profile.TokenSecret != "fake-add-token" || profile.Timeout != "1m0s" || profile.DefaultOutput != "table" {
				t.Fatal("add did not save the requested profile options and current selection")
			}
			if tc.existing && cfg.Profiles["home"] != original {
				t.Fatal("add must preserve existing profiles")
			}
			if stdout.Len() != 0 || strings.Contains(stderr.String(), "fake-add-token") {
				t.Fatal("prompts must use stderr without displaying the token")
			}
		})
	}
}

func TestConfigAddDuplicateNameIsRetried(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := config.Profile{Endpoint: "https://home.example:8006/api2/json", TokenID: "automation@pve!home", TokenSecretEnv: "PVE_TEST_TOKEN"}
	if err := config.Save(path, &config.Config{CurrentProfile: "home", Profiles: map[string]config.Profile{"home": original}}); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := RunWithDependencies([]string{"pve", "--config", path, "config", "add"}, "test", Dependencies{
		Stdin: strings.NewReader("\nlab\nhttps://lab.example:8006/api2/json\nautomation@pve!lab\nenv\nPVE_LAB_TEST_TOKEN\n\n\n"), Stdout: &stdout, Stderr: &stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profiles["home"] != original || cfg.Profiles["lab"].TokenSecretEnv != "PVE_LAB_TEST_TOKEN" || cfg.CurrentProfile != "home" || !strings.Contains(stderr.String(), "already exists; choose another name") {
		t.Fatal("a duplicate profile name must be retried without replacing the original")
	}
}

func TestConfigAddCancellationKeepsConfig(t *testing.T) {
	for _, existing := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		original := "current_profile: home\nprofiles: {}\n"
		if existing {
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		var stdout, stderr bytes.Buffer
		err := RunWithDependencies([]string{"pve", "--config", path, "config", "add"}, "test", Dependencies{
			Stdin: strings.NewReader("lab\nhttps://lab.example:8006/api2/json\nautomation@pve!lab\ntoken\nfake-unsaved-add-token\nno\n"), Stdout: &stdout, Stderr: &stderr,
		})
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if existing && (err != nil || string(data) != original) {
			t.Fatal("cancelled add must not modify existing config")
		}
		if !existing && !errors.Is(err, os.ErrNotExist) {
			t.Fatal("cancelled add must not create config")
		}
		if !strings.Contains(stderr.String(), "cancelled; no config file was changed") {
			t.Fatal("cancelled add must report cancellation")
		}
	}
}

func TestConfigAddReloadsConfigBeforeSaving(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(map[bool]string{false: "preserve other new profiles", true: "preserve duplicate profile"}[duplicate], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			cfg := &config.Config{CurrentProfile: "home", Profiles: map[string]config.Profile{
				"home": {Endpoint: "https://home.example:8006/api2/json", TokenID: "automation@pve!home", TokenSecretEnv: "PVE_TEST_TOKEN"},
			}}
			if err := config.Save(path, cfg); err != nil {
				t.Fatal(err)
			}
			concurrentName := "other"
			if duplicate {
				concurrentName = "lab"
			}
			concurrent := config.Profile{Endpoint: "https://other.example:8006/api2/json", TokenID: "automation@pve!other", TokenSecretEnv: "PVE_OTHER_TEST_TOKEN"}
			var stdout, stderr bytes.Buffer
			writer := configShowTestWriter(func(data []byte) (int, error) {
				if strings.Contains(string(data), "Set as current profile?") {
					cfg.Profiles[concurrentName] = concurrent
					cfg.CurrentProfile = concurrentName
					if err := config.Save(path, cfg); err != nil {
						return 0, err
					}
				}
				return stderr.Write(data)
			})
			err := RunWithDependencies([]string{"pve", "--config", path, "config", "add"}, "test", Dependencies{
				Stdin: strings.NewReader("lab\nhttps://lab.example:8006/api2/json\nautomation@pve!lab\ntoken\nfake-add-token\nno\nno\n"), Stdout: &stdout, Stderr: writer,
			})
			if duplicate && (err == nil || !strings.Contains(err.Error(), "existing profile was kept")) {
				t.Fatal("a duplicate created during input must block addition")
			}
			if !duplicate && err != nil {
				t.Fatal(err)
			}
			saved, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Profiles[concurrentName] != concurrent || saved.CurrentProfile != concurrentName {
				t.Fatal("add must preserve profiles and current selection changed during input")
			}
			if !duplicate && saved.Profiles["lab"].TokenSecret != "fake-add-token" {
				t.Fatal("add must append the new profile to reloaded config")
			}
		})
	}
}

func TestConfigAddKeepsFileCreatedDuringSetup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := "current_profile: other\nprofiles: {}\n"
	var stdout, stderr bytes.Buffer
	writer := configShowTestWriter(func(data []byte) (int, error) {
		if strings.Contains(string(data), "Set as current profile?") {
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				return 0, err
			}
		}
		return stderr.Write(data)
	})
	err := RunWithDependencies([]string{"pve", "--config", path, "config", "add"}, "test", Dependencies{
		Stdin: strings.NewReader("lab\nhttps://lab.example:8006/api2/json\nautomation@pve!lab\ntoken\nfake-add-token\nno\nyes\n"), Stdout: &stdout, Stderr: writer,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original || !strings.Contains(stderr.String(), "existing file was kept") {
		t.Fatal("add must not overwrite a config file created during input")
	}
}

func TestConfigAddRequiresTerminal(t *testing.T) {
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
	err = RunWithDependencies([]string{"pve", "--config", path, "config", "add"}, "test", Dependencies{Stdin: input, Stdout: &stdout, Stderr: &stderr})
	if err == nil || !strings.Contains(err.Error(), "config add requires a terminal") || !strings.Contains(err.Error(), "pve config update NAME --endpoint") {
		t.Fatal("nonterminal add must return a clear noninteractive setup hint")
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatal("nonterminal add must not prompt")
	}
}
