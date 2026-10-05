package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v2/internal/config"
	"github.com/lz-wang/pve-cli/v2/internal/output"
	"github.com/lz-wang/pve-cli/v2/internal/pve"
)

func TestConfigSetWithPlaintextToken(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	var stdout, stderr bytes.Buffer
	deps := Dependencies{
		Stdout: &stdout,
		Stderr: &stderr,
		BackendFactory: func(_ config.Profile, options pve.ClientOptions) (pve.Backend, error) {
			if options.TokenSecret != "fake-plaintext-token" {
				t.Fatal("backend did not receive the plaintext token")
			}
			return &commandBackend{nodes: []output.NodeRow{{Name: "pve1"}}}, nil
		},
	}
	if err := RunWithDependencies([]string{
		"pve", "--config", cfgPath, "config", "set", "home",
		"--endpoint", "https://pve.example:8006/api2/json",
		"--token-id", "automation@pve!test",
		"--token-secret", "fake-plaintext-token",
	}, "test", deps); err != nil {
		t.Fatalf("configure: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	profile := cfg.Profiles["home"]
	if profile.TokenSecret != "fake-plaintext-token" || profile.TokenSecretEnv != "" {
		t.Fatal("config did not store the plaintext token directly")
	}
	stdout.Reset()
	if err := RunWithDependencies([]string{"pve", "--config", cfgPath, "config", "show"}, "test", deps); err != nil {
		t.Fatalf("config show: %v", err)
	}
	shown := stdout.String()
	for _, want := range []string{"FIELD", "VALUE", "Profile", "Current", "Endpoint", "Token ID", "Token secret", "Token secret env", "Skip TLS verify", "Timeout", "Default output", "fak*****ken"} {
		if !strings.Contains(shown, want) {
			t.Fatalf("config show missing %q:\n%s", want, shown)
		}
	}
	if strings.Contains(shown+stderr.String(), "fake-plaintext-token") {
		t.Fatal("config show should summarize the stored plaintext field")
	}
	if !strings.HasSuffix(shown, "Config file: "+cfgPath+"\n") {
		t.Fatalf("config show should end with its absolute path:\n%s", shown)
	}
	stdout.Reset()
	if err := RunWithDependencies([]string{"pve", "--config", cfgPath, "node", "ls"}, "test", deps); err != nil {
		t.Fatalf("runtime: %v", err)
	}
	if !strings.Contains(stdout.String(), "pve1") {
		t.Fatal("node listing did not use the configured backend")
	}
	stdout.Reset()
	if err := RunWithDependencies([]string{"pve", "--config", cfgPath, "doctor", "--offline", "-o", "json"}, "test", deps); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if !strings.Contains(stdout.String(), "TOKEN_SECRET") || strings.Contains(stdout.String()+stderr.String(), "fake-plaintext-token") {
		t.Fatal("doctor should report the credential source without its value")
	}
}

func TestConfigSetRequiresTokenSource(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath, "config", "set", "home",
		"--endpoint", "https://pve.example:8006/api2/json",
		"--token-id", "automation@pve!test",
	}, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "token-secret or token-secret-env is required") {
		t.Fatalf("missing credential source: %v", err)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatal("invalid input should not write a config file")
	}
}

func TestConfigSetCommandWritesProfile(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")

	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath,
		"config", "set", "home",
		"--endpoint", "https://pve.example:8006/api2/json",
		"--token-id", "root@pam!test",
		"--token-secret-env", "PVE_TOKEN",
		"--insecure",
	}, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.CurrentProfile != "home" {
		t.Fatalf("current profile = %q, want home", cfg.CurrentProfile)
	}
	profile := cfg.Profiles["home"]
	if profile.Endpoint != "https://pve.example:8006/api2/json" || profile.TokenID != "root@pam!test" || profile.TokenSecretEnv != "PVE_TOKEN" {
		t.Fatalf("profile = %#v", profile)
	}
	if !profile.InsecureSkipVerify || profile.Timeout != "30s" || profile.DefaultOutput != "table" {
		t.Fatalf("profile defaults = %#v", profile)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(data)
	for _, want := range []string{"current_profile: home", "profiles:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("config yaml missing %q:\n%s", want, text)
		}
	}
	for _, old := range []string{"current_context", "contexts:"} {
		if strings.Contains(text, old) {
			t.Fatalf("config yaml includes old key %q:\n%s", old, text)
		}
	}
}

func TestConfigSetExistingProfileKeepsCurrent(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Profiles["lab"] = config.Profile{
		Endpoint:      "https://old.example:8006/api2/json",
		TokenID:       "root@pam!old",
		TokenSecret:   "old-placeholder-token",
		Timeout:       "10s",
		DefaultOutput: "json",
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	originalHome := cfg.Profiles["home"]
	err = RunWithDependencies([]string{
		"pve", "--config", cfgPath, "config", "set", "lab",
		"--endpoint", "https://new.example:8006/api2/json",
		"--token-id", "root@pam!new",
		"--token-secret-env", "PVE_LAB_TOKEN",
	}, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	cfg, err = config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentProfile != "home" || cfg.Profiles["home"] != originalHome {
		t.Fatal("updating lab should preserve the current home profile")
	}
	profile := cfg.Profiles["lab"]
	if profile.Endpoint != "https://new.example:8006/api2/json" || profile.TokenID != "root@pam!new" || profile.TokenSecretEnv != "PVE_LAB_TOKEN" || profile.TokenSecret != "" {
		t.Fatalf("updated lab = %#v", profile)
	}
	if profile.Timeout != "30s" || profile.DefaultOutput != "table" {
		t.Fatalf("updated profile defaults = %#v", profile)
	}
}

func TestConfigSetRequiredInputs(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "profile name", args: []string{"--endpoint", "https://pve.example:8006/api2/json", "--token-id", "root@pam!test", "--token-secret-env", "PVE_TOKEN"}},
		{name: "endpoint", args: []string{"home", "--token-id", "root@pam!test", "--token-secret-env", "PVE_TOKEN"}},
		{name: "token ID", args: []string{"home", "--endpoint", "https://pve.example:8006/api2/json", "--token-secret-env", "PVE_TOKEN"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), "config.yaml")
			args := append([]string{"pve", "--config", cfgPath, "config", "set"}, test.args...)
			if err := RunWithDependencies(args, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}); err == nil {
				t.Fatal("expected required input error")
			}
			if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
				t.Fatal("invalid input should not write a config file")
			}
		})
	}
}

func TestConfigProfileCommands(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath, "config", "set", "lab",
		"--endpoint", "https://pve-lab.example:8006/api2/json",
		"--token-id", "root@pam!test",
		"--token-secret-env", "PVE_LAB_TOKEN",
	}, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("create lab profile: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentProfile != "home" {
		t.Fatalf("adding lab changed current profile to %q", cfg.CurrentProfile)
	}

	var stdout bytes.Buffer
	deps := Dependencies{Stdout: &stdout, Stderr: &bytes.Buffer{}}
	err = RunWithDependencies([]string{"pve", "--config", cfgPath, "config", "show"}, "test", deps)
	if err != nil {
		t.Fatalf("show current profile: %v", err)
	}
	if !strings.Contains(stdout.String(), cfg.Profiles["home"].Endpoint) || strings.Contains(stdout.String(), "pve-lab.example") {
		t.Fatalf("default show should select current home profile:\n%s", stdout.String())
	}
	stdout.Reset()
	err = RunWithDependencies([]string{"pve", "--config", cfgPath, "config", "show", "lab"}, "test", deps)
	if err != nil {
		t.Fatalf("show named profile: %v", err)
	}
	if !strings.Contains(stdout.String(), "https://pve-lab.example:8006/api2/json") || strings.Contains(stdout.String(), cfg.Profiles["home"].Endpoint) {
		t.Fatalf("show lab should select only lab:\n%s", stdout.String())
	}
}

func TestConfigUseCommandSwitchesProfile(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	originalHome := cfg.Profiles["home"]
	originalLab := config.Profile{
		Endpoint:           "https://lab.example:8006/api2/json",
		TokenID:            "root@pam!lab",
		TokenSecret:        "lab-placeholder-token",
		TokenSecretEnv:     "PVE_LAB_TOKEN",
		InsecureSkipVerify: true,
		Timeout:            "45s",
		DefaultOutput:      "json",
	}
	cfg.Profiles["lab"] = originalLab
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	if err := RunWithDependencies([]string{"pve", "--config", cfgPath, "config", "use", "lab"}, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}); err != nil {
		t.Fatalf("use profile: %v", err)
	}
	cfg, err = config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentProfile != "lab" || cfg.Profiles["home"] != originalHome || cfg.Profiles["lab"] != originalLab {
		t.Fatal("switching to lab should preserve both profiles")
	}

	if err := RunWithDependencies([]string{"pve", "--config", cfgPath, "config", "use", "missing"}, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}); err == nil {
		t.Fatal("expected missing profile error")
	}
}

func TestConfigSetDoesNotSwitchCurrentProfile(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath, "config", "set", "lab",
		"--endpoint", "https://lab.example:8006/api2/json",
		"--token-id", "root@pam!lab",
		"--token-secret", "lab-placeholder-token",
	}, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentProfile != "home" {
		t.Fatal("set should not change the current profile")
	}
}

func TestRemovedConfigNamesAreRejected(t *testing.T) {
	if err := RunWithDependencies([]string{"pve", "--context", "home", "version"}, "test", Dependencies{
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	}); err == nil {
		t.Fatal("expected old --context flag to be rejected")
	}

	// Removed config subcommands (view, init, set-profile, current-profile,
	// use-profile, set-context, use-context, current-context, update) are no
	// longer registered at all; the CLI framework rejects them with its
	// "No help topic" error and exit code 3, which calls os.Exit from inside
	// urfave/cli and therefore cannot be exercised in-process here.
}

func TestDoctorOfflineCommand(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pve",
		"--config", cfgPath,
		"doctor", "--offline",
	}, "test", Dependencies{Stdout: &stdout, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "CONFIG_PATH") || !strings.Contains(got, "API_CONNECTIVITY") || !strings.Contains(got, "skip") {
		t.Fatalf("stdout = %s", got)
	}
}

func TestDoctorOfflineJSONCommand(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pve",
		"--config", cfgPath,
		"doctor", "--offline", "-o", "json",
	}, "test", Dependencies{Stdout: &stdout, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, `"check": "CONFIG_PATH"`) || !strings.Contains(got, `"status": "skip"`) {
		t.Fatalf("stdout = %s", got)
	}
}

func TestDoctorNodeCommand(t *testing.T) {
	cfgPath := writeTestConfig(t, "table")
	backend := &commandBackend{nodes: []output.NodeRow{{Name: "pve1"}}}
	var stdout bytes.Buffer

	err := RunWithDependencies([]string{
		"pve",
		"--config", cfgPath,
		"doctor", "--node", "pve1",
	}, "test", testDeps(&stdout, backend))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "NODE") || !strings.Contains(got, "node pve1 exists") {
		t.Fatalf("stdout = %s", got)
	}
}

func TestDoctorFailureReturnsError(t *testing.T) {
	var stdout bytes.Buffer
	err := RunWithDependencies([]string{
		"pve",
		"--config", filepath.Join(t.TempDir(), "missing.yaml"),
		"doctor", "--offline",
	}, "test", Dependencies{Stdout: &stdout, Stderr: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("expected doctor failure")
	}
	if err.Error() != "doctor checks failed" {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(stdout.String(), "CONFIG_FILE") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}
