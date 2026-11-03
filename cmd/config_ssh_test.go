package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigShowDisplaysSSHSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := `
current_profile: lab
profiles:
  lab:
    endpoint: https://pve8845:8006
    token_id: user@pam!cli
    token_secret_env: PVE_TOKEN
    ssh:
      host: pve8845
      user: root
      identity_file: ~/.ssh/id_ed25519
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := RunWithDependencies([]string{"pve", "--config", path, "config", "show"}, "test", Dependencies{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		t.Fatalf("config show: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"SSH host", "pve8845", "SSH user", "root", "SSH identity file", "~/.ssh/id_ed25519"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestConfigShowOmitsSSHWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := `
current_profile: lab
profiles:
  lab:
    endpoint: https://pve8845:8006
    token_id: user@pam!cli
    token_secret_env: PVE_TOKEN
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := RunWithDependencies([]string{"pve", "--config", path, "config", "show"}, "test", Dependencies{Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		t.Fatalf("config show: %v", err)
	}
	if strings.Contains(stdout.String(), "SSH host") {
		t.Fatalf("unexpected SSH section:\n%s", stdout.String())
	}
}
