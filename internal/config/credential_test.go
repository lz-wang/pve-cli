package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrivateCredential(t *testing.T) {
	dir := t.TempDir()
	path, err := StoreCredential(filepath.Join(dir, "config.yaml"), "private-token")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := ResolveTokenSecret(Profile{TokenSecretFile: path})
	if err != nil || secret != "private-token" {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 && runtime.GOOS != "windows" {
		t.Fatal("not private")
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if _, err = ReadCredential(path); err == nil {
			t.Fatal("insecure file accepted")
		}
	}
}
func TestRejectOldPlaintextConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("profiles:\n  home:\n    token_secret: fake-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("unknown field accepted")
	}
}
func TestCredentialEnvAndConflicts(t *testing.T) {
	t.Setenv("PVE_TEST_TOKEN", "env-token")
	s, e := ResolveTokenSecret(Profile{TokenSecretEnv: "PVE_TEST_TOKEN"})
	if e != nil || s != "env-token" {
		t.Fatal(e)
	}
	_, e = ResolveTokenSecret(Profile{TokenSecretEnv: "PVE_TEST_TOKEN", TokenSecretFile: "file"})
	if e == nil || !strings.Contains(e.Error(), "choose") {
		t.Fatal(e)
	}
}
