package cmd

import (
	"bytes"
	"github.com/urfave/cli/v2"
	"strings"
	"testing"
)

func TestV3CommandFlagsUniqueAndNoLegacyFlags(t *testing.T) {
	var walk func([]*cli.Command)
	walk = func(commands []*cli.Command) {
		for _, command := range commands {
			seen := map[string]bool{}
			for _, flag := range command.Flags {
				for _, name := range flag.Names() {
					if seen[name] {
						t.Errorf("%s duplicate %s", command.Name, name)
					}
					seen[name] = true
					if name == "force" || name == "token-secret" {
						t.Errorf("legacy flag %s", name)
					}
				}
			}
			if command.Name == "reboot" || command.Name == "shutdown" {
				if command.ArgsUsage == "NODE" && seen["wait"] {
					t.Fatal("node power must not wait")
				}
			}
			walk(command.Subcommands)
		}
	}
	walk(NewApp("test").Commands)
}
func TestUsageExitCodeAndCompletionFlags(t *testing.T) {
	for _, args := range [][]string{{"pve", "unknown"}, {"pve", "vm", "unknown"}, {"pve", "vm", "start"}, {"pve", "--bad"}, {"pve", "vm", "create", "100"}} {
		err := RunWithDependencies(args, "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
		if ExitCode(err) != 2 {
			t.Errorf("%v: %v code %d", args, err, ExitCode(err))
		}
	}
	var out bytes.Buffer
	err := RunWithDependencies([]string{"pve", "_complete", "vm", "get", "--output", "j"}, "test", Dependencies{Stdout: &out, Stderr: &bytes.Buffer{}})
	if err != nil || strings.TrimSpace(out.String()) != "json" {
		t.Fatalf("%q %v", out.String(), err)
	}
}
