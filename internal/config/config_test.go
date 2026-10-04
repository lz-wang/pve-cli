package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadAndUseProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Empty()

	if err := cfg.SetProfile("home", Profile{
		Endpoint:       "https://pve.lan:8006/api2/json",
		TokenID:        "automation@pve!pve",
		TokenSecretEnv: "PVE_HOME_TOKEN_SECRET",
		Timeout:        "30s",
		DefaultOutput:  "json",
	}); err != nil {
		t.Fatalf("set profile: %v", err)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.CurrentProfile != "home" {
		t.Fatalf("current profile = %q, want home", loaded.CurrentProfile)
	}

	name, profile, err := loaded.SelectProfile("")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if name != "home" || profile.TokenID != "automation@pve!pve" {
		t.Fatalf("selected %q/%q", name, profile.TokenID)
	}

	if err := loaded.UseProfile("missing"); err == nil {
		t.Fatal("expected missing profile error")
	}
}

func TestLoadOrEmptyMissingFile(t *testing.T) {
	cfg, err := LoadOrEmpty(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("load or empty: %v", err)
	}
	if cfg == nil || len(cfg.Profiles) != 0 {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

func TestInitProfileCreatesAndSetsCurrent(t *testing.T) {
	cfg := Empty()

	if err := cfg.InitProfile(InitOptions{
		Name:    "home",
		Profile: testProfile(),
		Use:     true,
	}); err != nil {
		t.Fatalf("init profile: %v", err)
	}

	if cfg.CurrentProfile != "home" {
		t.Fatalf("current profile = %q, want home", cfg.CurrentProfile)
	}
	if got := cfg.Profiles["home"].Endpoint; got != "https://pve.lan:8006/api2/json" {
		t.Fatalf("endpoint = %q", got)
	}
}

func TestInitProfileNoUseLeavesCurrentEmpty(t *testing.T) {
	cfg := Empty()

	if err := cfg.InitProfile(InitOptions{
		Name:    "home",
		Profile: testProfile(),
		Use:     false,
	}); err != nil {
		t.Fatalf("init profile: %v", err)
	}

	if cfg.CurrentProfile != "" {
		t.Fatalf("current profile = %q, want empty", cfg.CurrentProfile)
	}
	if _, ok := cfg.Profiles["home"]; !ok {
		t.Fatal("expected profile to be created")
	}
}

func TestInitProfileRejectsExistingWithoutOverwrite(t *testing.T) {
	cfg := Empty()
	if err := cfg.InitProfile(InitOptions{Name: "home", Profile: testProfile(), Use: true}); err != nil {
		t.Fatalf("init profile: %v", err)
	}

	replacement := testProfile()
	replacement.Endpoint = "https://other.example:8006/api2/json"
	if err := cfg.InitProfile(InitOptions{Name: "home", Profile: replacement, Use: true}); err == nil {
		t.Fatal("expected existing profile error")
	}
	if got := cfg.Profiles["home"].Endpoint; got != "https://pve.lan:8006/api2/json" {
		t.Fatalf("endpoint changed without overwrite: %q", got)
	}
}

func TestInitProfileOverwritesExisting(t *testing.T) {
	cfg := Empty()
	if err := cfg.InitProfile(InitOptions{Name: "home", Profile: testProfile(), Use: true}); err != nil {
		t.Fatalf("init profile: %v", err)
	}

	replacement := testProfile()
	replacement.Endpoint = "https://other.example:8006/api2/json"
	if err := cfg.InitProfile(InitOptions{
		Name:      "home",
		Profile:   replacement,
		Overwrite: true,
		Use:       true,
	}); err != nil {
		t.Fatalf("overwrite profile: %v", err)
	}
	if got := cfg.Profiles["home"].Endpoint; got != "https://other.example:8006/api2/json" {
		t.Fatalf("endpoint = %q", got)
	}
}

func TestInitProfileValidatesRequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Profile)
		wantErr string
	}{
		{
			name:    "endpoint",
			mutate:  func(profile *Profile) { profile.Endpoint = "" },
			wantErr: "endpoint is required",
		},
		{
			name:    "token id",
			mutate:  func(profile *Profile) { profile.TokenID = "" },
			wantErr: "token-id is required",
		},
		{
			name:    "token secret env",
			mutate:  func(profile *Profile) { profile.TokenSecretEnv = "" },
			wantErr: "token-secret-env is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := testProfile()
			tt.mutate(&profile)
			err := Empty().InitProfile(InitOptions{Name: "home", Profile: profile, Use: true})
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestResolveTokenSecret(t *testing.T) {
	t.Setenv("PVE_TEST_TOKEN", "secret")

	secret, err := ResolveTokenSecret(Profile{TokenSecretEnv: "PVE_TEST_TOKEN"})
	if err != nil {
		t.Fatalf("resolve secret: %v", err)
	}
	if secret != "secret" {
		t.Fatalf("secret = %q", secret)
	}

	os.Unsetenv("PVE_TEST_TOKEN")
	if _, err := ResolveTokenSecret(Profile{TokenSecretEnv: "PVE_TEST_TOKEN"}); err == nil {
		t.Fatal("expected missing env error")
	}
}

func testProfile() Profile {
	return Profile{
		Endpoint:       "https://pve.lan:8006/api2/json",
		TokenID:        "automation@pve!pve",
		TokenSecretEnv: "PVE_HOME_TOKEN_SECRET",
		Timeout:        "30s",
		DefaultOutput:  "table",
	}
}
