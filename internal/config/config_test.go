package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
			wantErr: "token-secret or token-secret-env is required",
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
	t.Setenv("PVE_TEST_TOKEN", "fake-environment-token")
	t.Setenv("PVE_EMPTY_TEST_TOKEN", "")
	cases := []struct {
		name    string
		profile Profile
		want    string
		wantErr string
	}{
		{name: "environment", profile: Profile{TokenSecretEnv: "PVE_TEST_TOKEN"}, want: "fake-environment-token"},
		{name: "plaintext", profile: Profile{TokenSecret: "fake-plaintext-token"}, want: "fake-plaintext-token"},
		{name: "plaintext wins", profile: Profile{TokenSecret: "fake-plaintext-token", TokenSecretEnv: "PVE_TEST_TOKEN"}, want: "fake-plaintext-token"},
		{name: "plaintext ignores empty environment", profile: Profile{TokenSecret: "fake-plaintext-token", TokenSecretEnv: "PVE_EMPTY_TEST_TOKEN"}, want: "fake-plaintext-token"},
		{name: "empty plaintext falls back", profile: Profile{TokenSecret: "", TokenSecretEnv: "PVE_TEST_TOKEN"}, want: "fake-environment-token"},
		{name: "missing source", wantErr: "token_secret or token_secret_env is required"},
		{name: "empty environment", profile: Profile{TokenSecretEnv: "PVE_EMPTY_TEST_TOKEN"}, wantErr: "environment variable PVE_EMPTY_TEST_TOKEN is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			secret, err := ResolveTokenSecret(tc.profile)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || secret != tc.want {
				t.Fatalf("resolved token mismatch: error = %v", err)
			}
		})
	}
}

func TestPlaintextProfileSaveLoadAndReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	// Saving over an existing file must also restrict its permissions.
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	profile := testProfile()
	profile.TokenSecret = "fake-token: with # YAML characters\nand a newline"
	profile.TokenSecretEnv = ""
	cfg := Empty()
	if err := cfg.InitProfile(InitOptions{Name: "home", Profile: profile, Use: true}); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := ResolveTokenSecret(loaded.Profiles["home"])
	if err != nil || secret != profile.TokenSecret {
		t.Fatalf("plaintext token did not survive save/load: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %o, want 600", info.Mode().Perm())
	}
	data, err := ToYAML(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "token_secret:") || strings.Contains(string(data), "token_secret_env:") {
		t.Fatal("plaintext configuration should contain token_secret only")
	}
	if err := loaded.SetProfile("home", testProfile()); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, loaded); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "token_secret:") || !strings.Contains(string(data), "token_secret_env:") {
		t.Fatal("switching to an environment reference should remove the plaintext token")
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
