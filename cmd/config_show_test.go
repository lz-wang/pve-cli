package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/config"
)

func TestConfigShowSelectsProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{CurrentProfile: "home", Profiles: map[string]config.Profile{
		"home": {Endpoint: "https://home.example:8006/api2/json", TokenSecretFile: "home-placeholder-token"},
		"lab":  {Endpoint: "https://lab.example:8006/api2/json", TokenSecretEnv: "PVE_LAB_TOKEN"},
	}}
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, selected string
		global, args   []string
	}{
		{name: "current", selected: "home"},
		{name: "named", selected: "lab", args: []string{"lab"}},
		{name: "global selector", selected: "lab", global: []string{"--profile", "lab"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			args := append([]string{"pve", "--config", path}, tc.global...)
			args = append(args, "config", "show")
			args = append(args, tc.args...)
			if err := RunWithDependencies(args, "test", Dependencies{Stdout: &stdout, Stderr: &bytes.Buffer{}}); err != nil {
				t.Fatalf("show: %v", err)
			}
			tables := parseConfigShowTables(t, stdout.String())
			if len(tables) != 1 || tables[0]["Profile"] != tc.selected || tables[0]["Endpoint"] != cfg.Profiles[tc.selected].Endpoint {
				t.Fatal("show should display only the selected profile")
			}
		})
	}
	for _, args := range [][]string{
		{"config", "show", "missing"},
		{"config", "show", "home", "lab"},
		{"config", "show", "home", "--all"},
		{"--profile", "home", "config", "show", "--all"},
		{"--profile", "home", "config", "show", "lab"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout bytes.Buffer
			if err := RunWithDependencies(append([]string{"pve", "--config", path}, args...), "test", Dependencies{Stdout: &stdout, Stderr: &bytes.Buffer{}}); err == nil {
				t.Fatal("missing profiles and conflicting selectors should fail")
			}
			if stdout.Len() != 0 {
				t.Fatal("invalid selection should not display partial profile data")
			}
		})
	}
}

func TestConfigListShowsOnlyNamesAndEndpoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(path, &config.Config{CurrentProfile: "zeta", Profiles: map[string]config.Profile{
		"zeta":  {Endpoint: "https://zeta.example:8006/api2/json", TokenID: "private-token-id", TokenSecretFile: "fake-private-secret"},
		"alpha": {Endpoint: "https://alpha.example:8006/api2/json", TokenSecretEnv: "PRIVATE_TOKEN_ENV"},
	}}); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := RunWithDependencies([]string{"pve", "--config", path, "config", "ls"}, "test", Dependencies{Stdout: &stdout, Stderr: &bytes.Buffer{}}); err != nil {
		t.Fatalf("ls: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 3 || strings.Join(strings.Fields(lines[0]), " ") != "PROFILE ENDPOINT" ||
		strings.Join(strings.Fields(lines[1]), " ") != "alpha https://alpha.example:8006/api2/json" ||
		strings.Join(strings.Fields(lines[2]), " ") != "zeta https://zeta.example:8006/api2/json" {
		t.Fatal("ls should contain only name and endpoint columns in name order")
	}
}

func TestConfigShowPrintsResolvedConfigPath(t *testing.T) {
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
	t.Setenv("PVE_SHOW_CONFIG_PATH", path)
	for _, tc := range []struct{ name, input string }{
		{"absolute", path},
		{"relative", relative},
		{"home expansion", "~/" + fromHome},
		{"environment expansion", "$PVE_SHOW_CONFIG_PATH"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			if err := RunWithDependencies([]string{"pve", "--config", tc.input, "config", "show", "--all"}, "test", Dependencies{Stdout: &stdout, Stderr: &bytes.Buffer{}}); err != nil {
				t.Fatalf("show: %v", err)
			}
			if !strings.HasSuffix(stdout.String(), "\nConfig file: "+path+"\n") {
				t.Fatal("the last line should show the expanded absolute config path")
			}
		})
	}
	var stdout bytes.Buffer
	if err := RunWithDependencies([]string{"pve", "--config", path, "config", "show"}, "test", Dependencies{Stdout: &stdout, Stderr: &bytes.Buffer{}}); err == nil {
		t.Fatal("show without a selector should report a missing current profile")
	}
}

func TestConfigShowKeepsPathLineBreaksOnOneLine(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Windows filenames cannot contain ASCII line breaks")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config\r\n\u0085\u2028\u2029.yaml")
	if err := config.Save(path, config.Empty()); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := RunWithDependencies([]string{"pve", "--config", path, "config", "show", "--all"}, "test", Dependencies{Stdout: &stdout, Stderr: &bytes.Buffer{}}); err != nil {
		t.Fatalf("show: %v", err)
	}
	wantPath := filepath.Join(dir, "config\\r\\n\\u0085\\u2028\\u2029.yaml")
	if !strings.HasSuffix(stdout.String(), "\nConfig file: "+wantPath+"\n") {
		t.Fatal("line breaks in filenames should be escaped in the final path line")
	}
}

func parseConfigShowTables(t *testing.T, text string) []map[string]string {
	t.Helper()
	var tables []map[string]string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line == "" || strings.HasPrefix(line, "Config file: ") {
			continue
		}
		split := strings.Index(line, "  ")
		if split < 0 {
			t.Fatalf("expected a two-column table row: %q", line)
		}
		field, value := line[:split], strings.TrimSpace(line[split:])
		if field == "FIELD" {
			if value != "VALUE" {
				t.Fatalf("unexpected table header: %q", line)
			}
			tables = append(tables, make(map[string]string))
			continue
		}
		if len(tables) == 0 {
			t.Fatal("profile fields should follow a table header")
		}
		tables[len(tables)-1][field] = value
	}
	return tables
}
