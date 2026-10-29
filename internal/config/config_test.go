package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
			name:    "token secret source",
			mutate:  func(profile *Profile) { profile.TokenSecretEnv = "" },
			wantErr: "token-secret-file or token-secret-env is required",
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

func TestSaveNewDoesNotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	cfg := Empty()
	if err := cfg.InitProfile(InitOptions{Name: "home", Profile: testProfile(), Use: true}); err != nil {
		t.Fatal(err)
	}
	if err := SaveNew(path, cfg); err != nil {
		t.Fatalf("create config: %v", err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %o, want 600", info.Mode().Perm())
	}
	cfg.CurrentProfile = "different"
	if err := SaveNew(path, cfg); !errors.Is(err, os.ErrExist) {
		t.Fatalf("second creation error = %v, want file exists", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatal("existing config was changed by SaveNew")
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
