package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/lz-wang/pvectl/internal/config"
	"github.com/lz-wang/pvectl/internal/pve"
)

func TestConfigViewSummarizesAllTokensWithoutChangingFile(t *testing.T) {
	cases := []struct {
		name, secret, summary string
	}{
		{"long", "dd1-hidden-example-token-ef4", "dd1*****ef4"},
		{"seven characters", "abcXxyz", "abc*****xyz"},
		{"six characters", "abcxyz", "*****"},
		{"one character", "x", "*****"},
		{"unicode", "甲乙丙隐藏丁戊己", "甲乙丙*****丁戊己"},
		{"short unicode", "甲乙丙丁", "*****"},
		{"environment only", "", ""},
	}
	cfg := config.Empty()
	cfg.CurrentProfile = "long"
	for _, tc := range cases {
		cfg.Profiles[tc.name] = config.Profile{
			Endpoint: "https://pve.example:8006/api2/json", TokenID: "automation@pve!test",
			TokenSecret: tc.secret, TokenSecretEnv: "PVE_VIEW_TOKEN",
			Timeout: "30s", DefaultOutput: "table", InsecureSkipVerify: true,
		}
	}
	t.Setenv("PVE_VIEW_TOKEN", "fake-environment-secret-not-for-display")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err = RunWithDependencies([]string{"pve", "--config", path, "config", "view"}, "test", Dependencies{
		Stdout: &stdout, Stderr: &stderr,
		BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) {
			t.Fatal("view must not contact Proxmox")
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatal("existing config should display on stdout without prompts")
	}
	var view config.Config
	if err := yaml.Unmarshal(stdout.Bytes(), &view); err != nil {
		t.Fatalf("view must remain valid YAML: %v", err)
	}
	if view.CurrentProfile != cfg.CurrentProfile || len(view.Profiles) != len(cfg.Profiles) {
		t.Fatal("view should preserve the profile selection and all profiles")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := cfg.Profiles[tc.name]
			want.TokenSecret = tc.summary
			if view.Profiles[tc.name] != want {
				t.Fatal("view should summarize the token and preserve other profile fields")
			}
		})
	}
	if strings.Contains(stdout.String(), "hidden-example-token") || strings.Contains(stdout.String(), "fake-environment-secret-not-for-display") {
		t.Fatal("view must not reveal hidden token contents or resolve environment values")
	}
	if !strings.HasSuffix(stdout.String(), "\n# Config file: "+path+"\n") {
		t.Fatal("the final output line should identify the config file")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("view must not modify the stored plaintext tokens or config file")
	}
}

func TestConfigViewPrintsResolvedConfigPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(path, config.Empty()); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, path)
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	fromHome, err := filepath.Rel(home, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PVE_VIEW_CONFIG_PATH", path)
	for _, tc := range []struct{ name, input string }{
		{"absolute", path},
		{"relative", relative},
		{"home expansion", "~/" + fromHome},
		{"environment expansion", "$PVE_VIEW_CONFIG_PATH"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := RunWithDependencies([]string{"pve", "--config", tc.input, "config", "view"}, "test", Dependencies{Stdout: &stdout, Stderr: &stderr}); err != nil {
				t.Fatalf("view: %v", err)
			}
			if !strings.HasSuffix(stdout.String(), "\n# Config file: "+path+"\n") {
				t.Fatal("the last line should show the expanded absolute config path")
			}
			var view config.Config
			if err := yaml.Unmarshal(stdout.Bytes(), &view); err != nil {
				t.Fatalf("empty config output should remain valid YAML: %v", err)
			}
		})
	}
}

func TestConfigViewKeepsPathLineBreaksInsideComment(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Windows filenames cannot contain ASCII line breaks")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config\r\n\u0085\u2028\u2029.yaml")
	if err := config.Save(path, config.Empty()); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := RunWithDependencies([]string{"pve", "--config", path, "config", "view"}, "test", Dependencies{Stdout: &stdout, Stderr: &stderr}); err != nil {
		t.Fatalf("view: %v", err)
	}
	wantPath := filepath.Join(dir, "config\\r\\n\\u0085\\u2028\\u2029.yaml")
	if !strings.HasSuffix(stdout.String(), "\n# Config file: "+wantPath+"\n") {
		t.Fatal("line breaks in filenames should be escaped within the final comment")
	}
	var view config.Config
	if err := yaml.Unmarshal(stdout.Bytes(), &view); err != nil {
		t.Fatalf("a config path must not break the YAML output: %v", err)
	}
}
