package cmd

import (
	"bytes"
	"github.com/lz-wang/pve-cli/v3/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func storedTestSecret(t *testing.T, p config.Profile) string {
	t.Helper()
	if p.TokenSecretFile == "" {
		return ""
	}
	s, e := config.ReadCredential(p.TokenSecretFile)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestConfigCredentialStdinPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	var out bytes.Buffer
	deps := Dependencies{Stdin: strings.NewReader("private-test-token\n"), Stdout: &out, Stderr: &out}
	err := RunWithDependencies([]string{"pve", "--config", path, "config", "set", "home", "--endpoint", "https://pve.example/api2/json", "--token-id", "a@pve!b", "--token-secret-stdin"}, "test", deps)
	if err != nil {
		t.Fatal(err)
	}
	cfg, e := config.Load(path)
	if e != nil {
		t.Fatal(e)
	}
	if storedTestSecret(t, cfg.Profiles["home"]) != "private-test-token" {
		t.Fatal("missing credential")
	}
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(data), "private-test-token") || strings.Contains(out.String(), "private-test-token") {
		t.Fatal("secret leaked")
	}
	out.Reset()
	if e = RunWithDependencies([]string{"pve", "--config", path, "config", "show"}, "test", deps); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.String(), "private-test-token") {
		t.Fatal("show leaked secret")
	}
}
