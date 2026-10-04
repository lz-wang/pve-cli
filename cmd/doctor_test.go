package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lz-wang/pvectl/internal/config"
	"github.com/lz-wang/pvectl/internal/output"
)

func TestConfigInitCommandWritesProfile(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")

	err := RunWithDependencies([]string{
		"pve",
		"--config", cfgPath,
		"config", "init",
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
	if profile.Endpoint != "https://pve.example:8006/api2/json" || profile.TokenSecretEnv != "PVE_TOKEN" {
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

func TestConfigInitNoUseLeavesCurrentEmpty(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")

	err := RunWithDependencies([]string{
		"pve",
		"--config", cfgPath,
		"config", "init",
		"--endpoint", "https://pve.example:8006/api2/json",
		"--token-id", "root@pam!test",
		"--token-secret-env", "PVE_TOKEN",
		"--no-use",
	}, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.CurrentProfile != "" {
		t.Fatalf("current profile = %q, want empty", cfg.CurrentProfile)
	}
	if _, ok := cfg.Profiles["home"]; !ok {
		t.Fatal("expected home profile")
	}
}

func TestConfigInitOverwriteBehavior(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	baseArgs := []string{
		"pve",
		"--config", cfgPath,
		"config", "init",
		"--endpoint", "https://pve.example:8006/api2/json",
		"--token-id", "root@pam!test",
		"--token-secret-env", "PVE_TOKEN",
	}
	if err := RunWithDependencies(baseArgs, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}); err != nil {
		t.Fatalf("run initial init: %v", err)
	}

	replacementArgs := []string{
		"pve",
		"--config", cfgPath,
		"config", "init",
		"--endpoint", "https://other.example:8006/api2/json",
		"--token-id", "root@pam!test",
		"--token-secret-env", "PVE_TOKEN",
	}
	if err := RunWithDependencies(replacementArgs, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}); err == nil {
		t.Fatal("expected duplicate profile error")
	}

	replacementArgs = append(replacementArgs, "--overwrite")
	if err := RunWithDependencies(replacementArgs, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}); err != nil {
		t.Fatalf("run overwrite: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := cfg.Profiles["home"].Endpoint; got != "https://other.example:8006/api2/json" {
		t.Fatalf("endpoint = %q", got)
	}
}

func TestConfigInitRequiredFlagFailure(t *testing.T) {
	err := RunWithDependencies([]string{
		"pve",
		"--config", filepath.Join(t.TempDir(), "config.yaml"),
		"config", "init",
	}, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("expected required flag error")
	}
}

func TestConfigProfileCommands(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")

	err := RunWithDependencies([]string{
		"pve",
		"--config", cfgPath,
		"config", "set-profile", "lab",
		"--endpoint", "https://pve-lab.example:8006/api2/json",
		"--token-id", "root@pam!test",
		"--token-secret-env", "PVE_TOKEN",
	}, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("set profile: %v", err)
	}

	err = RunWithDependencies([]string{
		"pve",
		"--config", cfgPath,
		"config", "use-profile", "lab",
	}, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("use profile: %v", err)
	}

	var stdout bytes.Buffer
	err = RunWithDependencies([]string{
		"pve",
		"--config", cfgPath,
		"config", "current-profile",
	}, "test", Dependencies{Stdout: &stdout, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("current profile: %v", err)
	}
	if got, want := stdout.String(), "lab\n"; got != want {
		t.Fatalf("current profile = %q, want %q", got, want)
	}
}

func TestOldContextNamesAreRejected(t *testing.T) {
	if err := RunWithDependencies([]string{"pve", "--context", "home", "version"}, "test", Dependencies{
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	}); err == nil {
		t.Fatal("expected old --context flag to be rejected")
	}

	for _, command := range []string{"set-context", "use-context", "current-context"} {
		err := RunWithDependencies([]string{
			"pve",
			"--config", filepath.Join(t.TempDir(), "config.yaml"),
			"config", command, "home",
		}, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		if err == nil {
			t.Fatalf("expected old %s command to be rejected", command)
		}
		if !strings.Contains(err.Error(), "was removed") {
			t.Fatalf("old %s error = %v", command, err)
		}
	}
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
