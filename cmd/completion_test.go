package cmd

import (
	"bytes"
	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"strings"
	"testing"
)

func TestCompletionScriptsAndStaticFallback(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		var out bytes.Buffer
		deps := Dependencies{Stdout: &out, Stderr: &bytes.Buffer{}, BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) {
			t.Fatal("static completion must not connect")
			return nil, nil
		}}
		if err := RunWithDependencies([]string{"pve", "completion", shell}, "test", deps); err != nil || !strings.Contains(out.String(), "_complete") {
			t.Fatalf("%s %v", shell, err)
		}
	}
	var out bytes.Buffer
	if err := RunWithDependencies([]string{"pve", "_complete", "vm", "st"}, "test", Dependencies{Stdout: &out, Stderr: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "start") || !strings.Contains(out.String(), "stop") {
		t.Fatal(out.String())
	}
}
