package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v2/internal/config"
	"github.com/lz-wang/pve-cli/v2/internal/pve"
)

func TestConfigShowSummarizesAllTokensWithoutChangingFile(t *testing.T) {
	cases := []struct{ name, secret, summary string }{
		{"long", "dd1-hidden-example-token-ef4", "dd1*****ef4"},
		{"seven characters", "abcXxyz", "abc*****xyz"},
		{"six characters", "abcxyz", "*****"},
		{"one character", "x", "*****"},
		{"unicode", "甲乙丙隐藏丁戊己", "甲乙丙*****丁戊己"},
		{"short unicode", "甲乙丙丁", "*****"},
		{"environment only", "", "-"},
	}
	cfg := config.Empty()
	cfg.CurrentProfile = "long"
	for _, tc := range cases {
		cfg.Profiles[tc.name] = config.Profile{
			Endpoint: "https://pve.example:8006/api2/json", TokenID: "automation@pve!test",
			TokenSecret: tc.secret, TokenSecretEnv: "PVE_SHOW_TOKEN",
			Timeout: "30s", DefaultOutput: "table", InsecureSkipVerify: true,
		}
	}
	t.Setenv("PVE_SHOW_TOKEN", "fake-environment-secret-not-for-display")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err = RunWithDependencies([]string{"pve", "--config", path, "config", "show", "--all"}, "test", Dependencies{
		Stdout: &stdout, Stderr: &stderr,
		BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) {
			t.Fatal("show must not contact Proxmox")
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatal("existing config should display on stdout without prompts")
	}
	tables := parseConfigShowTables(t, stdout.String())
	if len(tables) != len(cases) {
		t.Fatal("show --all should display every profile")
	}
	actual := make(map[string]map[string]string)
	var names []string
	for _, table := range tables {
		actual[table["Profile"]] = table
		names = append(names, table["Profile"])
	}
	if strings.Join(names, ",") != "environment only,long,one character,seven characters,short unicode,six characters,unicode" {
		t.Fatal("all profiles should appear in name order")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := actual[tc.name]
			if row["Token secret"] != tc.summary || row["Token secret env"] != "PVE_SHOW_TOKEN" {
				t.Fatal("show should summarize plaintext tokens and retain environment references")
			}
			if row["Endpoint"] != cfg.Profiles[tc.name].Endpoint || row["Token ID"] != "automation@pve!test" ||
				row["Skip TLS verify"] != "yes" || row["Timeout"] != "30s" || row["Default output"] != "table" {
				t.Fatal("show should retain all other profile details")
			}
			if (row["Current"] == "yes") != (tc.name == cfg.CurrentProfile) {
				t.Fatal("show should identify the current profile")
			}
		})
	}
	if strings.Contains(stdout.String(), "hidden-example-token") || strings.Contains(stdout.String(), "fake-environment-secret-not-for-display") {
		t.Fatal("show must not reveal hidden token contents or resolve environment values")
	}
	if !strings.HasSuffix(stdout.String(), "\nConfig file: "+path+"\n") {
		t.Fatal("the final output line should identify the config file")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("show must not modify the stored plaintext tokens or config file")
	}
}

func TestConfigShowSelectsProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &config.Config{CurrentProfile: "home", Profiles: map[string]config.Profile{
		"home": {Endpoint: "https://home.example:8006/api2/json", TokenSecret: "home-placeholder-token"},
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
		"zeta":  {Endpoint: "https://zeta.example:8006/api2/json", TokenID: "private-token-id", TokenSecret: "fake-private-secret"},
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
