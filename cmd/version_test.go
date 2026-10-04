package cmd

import (
	"bytes"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/lz-wang/pvectl/internal/config"
	"github.com/lz-wang/pvectl/internal/pve"
)

func TestVersionCommandFormatsWithoutConfigOrBackend(t *testing.T) {
	build := BuildInfo{
		Version: "v1.0.0",
		Commit:  "abc1234",
		Date:    "2026-06-06T00:00:00Z",
	}

	cases := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:   "table",
			format: "table",
			contains: []string{
				"VERSION",
				"v1.0.0",
				"abc1234",
				goruntime.Version(),
			},
		},
		{
			name:   "yaml",
			format: "yaml",
			contains: []string{
				"version: v1.0.0",
				"commit: abc1234",
				"date: \"2026-06-06T00:00:00Z\"",
				"go_version: " + goruntime.Version(),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			err := RunWithBuildInfoAndDependencies([]string{
				"pve",
				"--config", filepath.Join(t.TempDir(), "missing.yaml"),
				"version",
				"-o", tc.format,
			}, build, Dependencies{
				Stdout: &stdout,
				Stderr: &stderr,
				BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) {
					t.Fatal("version command must not initialize backend")
					return nil, nil
				},
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if stderr.String() != "" {
				t.Fatalf("stderr = %s", stderr.String())
			}
			for _, want := range tc.contains {
				if !strings.Contains(stdout.String(), want) {
					t.Fatalf("stdout missing %q in %s", want, stdout.String())
				}
			}
		})
	}
}

func TestVersionFlagKeepsCompactOutput(t *testing.T) {
	var stdout bytes.Buffer
	err := RunWithBuildInfoAndDependencies([]string{"pve", "--version"}, BuildInfo{Version: "v1.0.0"}, Dependencies{
		Stdout: &stdout,
		Stderr: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got, want := stdout.String(), "pve version v1.0.0\n"; got != want {
		t.Fatalf("--version output = %q, want %q", got, want)
	}
}
