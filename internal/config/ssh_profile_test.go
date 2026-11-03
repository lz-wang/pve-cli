package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/ssh"
)

func writeSSHConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const sshProfileYAML = `
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

func TestProfileSSHOptionalSection(t *testing.T) {
	cfg, err := Load(writeSSHConfig(t, sshProfileYAML))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	profile := cfg.Profiles["lab"]
	if profile.SSH == nil {
		t.Fatal("ssh section missing")
	}
	if profile.SSH.Host != "pve8845" || profile.SSH.User != "root" || profile.SSH.IdentityFile != "~/.ssh/id_ed25519" {
		t.Fatalf("ssh = %+v", profile.SSH)
	}
	if err := profile.SSH.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestProfileWithoutSSHSection(t *testing.T) {
	cfg, err := Load(writeSSHConfig(t, `
profiles:
  lab:
    endpoint: https://pve8845:8006
    token_id: user@pam!cli
    token_secret_env: PVE_TOKEN
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Profiles["lab"].SSH != nil {
		t.Fatalf("ssh = %+v, want nil", cfg.Profiles["lab"].SSH)
	}
}

func TestProfileSSHStrictSchema(t *testing.T) {
	err := mustLoadErr(t, `
profiles:
  lab:
    endpoint: https://pve8845:8006
    token_id: user@pam!cli
    token_secret_env: PVE_TOKEN
    ssh:
      host: pve8845
      strict_host_key_checking: false
`)
	if err == nil || !strings.Contains(err.Error(), "strict_host_key_checking") {
		t.Fatalf("err = %v, want unknown field rejected", err)
	}
}

// There must be no way to configure host key verification off.
func TestProfileSSHHasNoDisableSwitch(t *testing.T) {
	err := mustLoadErr(t, `
profiles:
  lab:
    endpoint: https://pve8845:8006
    token_id: user@pam!cli
    token_secret_env: PVE_TOKEN
    ssh:
      host: pve8845
      insecure: true
`)
	if err == nil || !strings.Contains(err.Error(), "insecure") {
		t.Fatalf("err = %v, want unknown field rejected", err)
	}
}

// Load parses leniently (validation happens on profile write/use); an
// incomplete ssh section must still be detectable via Validate.
func TestProfileSSHValidation(t *testing.T) {
	cfg, err := Load(writeSSHConfig(t, `
profiles:
  lab:
    endpoint: https://pve8845:8006
    token_id: user@pam!cli
    token_secret_env: PVE_TOKEN
    ssh:
      user: root
`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := cfg.Profiles["lab"].SSH.Validate(); err == nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("err = %v, want missing host", err)
	}
}

func TestSetProfileValidatesSSH(t *testing.T) {
	cfg := Empty()
	err := cfg.SetProfile("lab", Profile{
		Endpoint:       "https://pve8845:8006",
		TokenID:        "user@pam!cli",
		TokenSecretEnv: "PVE_TOKEN",
		SSH:            &ssh.Profile{Host: "pve8845", Port: 99999},
	})
	if err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("err = %v, want port rejection", err)
	}
}

func mustLoadErr(t *testing.T, content string) error {
	t.Helper()
	_, err := Load(writeSSHConfig(t, content))
	return err
}
