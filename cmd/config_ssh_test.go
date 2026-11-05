package cmd

import (
	"bytes"
	"github.com/lz-wang/pve-cli/v3/internal/config"
	"os"
	"path/filepath"
	"reflect"
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

func TestConfigSSHSetPreservesProfileAndClear(t *testing.T) {
	path := writeTwoProfileConfig(t)
	before, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	original := before.Profiles["n150"]
	run := func(args ...string) error {
		return RunWithDependencies(append([]string{"pve", "--config", path, "config", "ssh"}, args...), "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	}
	if err = run("set", "n150", "--host", "lab.example", "--node", "n150", "--port", "2222", "--identity-file", "~/.ssh/lab"); err != nil {
		t.Fatal(err)
	}
	if err = run("set", "n150", "--user", "operator"); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Profiles["n150"]
	if p.SSH.Host != "lab.example" || p.SSH.Node != "n150" || p.SSH.User != "operator" || p.SSH.Port != 2222 {
		t.Fatalf("%+v", p.SSH)
	}
	p.SSH = nil
	if !reflect.DeepEqual(p, original) || cfg.CurrentProfile != before.CurrentProfile || !reflect.DeepEqual(cfg.Profiles["pve8845"], before.Profiles["pve8845"]) {
		t.Fatal("unrelated profile data changed")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("config permissions changed")
	}
	saved, _ := os.ReadFile(path)
	for _, args := range [][]string{{"set", "missing", "--host", "lab"}, {"set", "n150", "--port", "0"}, {"set", "n150", "--host", "-oProxyCommand=bad"}, {"set", "n150", "--node", ""}, {"set", "n150"}} {
		if err = run(args...); err == nil {
			t.Fatalf("accepted %v", args)
		}
		now, _ := os.ReadFile(path)
		if !bytes.Equal(now, saved) {
			t.Fatal("invalid update wrote config")
		}
	}
	if err = run("clear", "n150"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = config.Load(path)
	if !reflect.DeepEqual(cfg.Profiles["n150"], original) {
		t.Fatal("clear changed API profile")
	}
}
