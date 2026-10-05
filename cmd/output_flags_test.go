package cmd

import (
	"strings"
	"testing"

	"github.com/urfave/cli/v2"
)

// structuredOutputCommands is the allowlist of leaf commands that write
// structured stdout and therefore accept -o/--output. Every other leaf
// command must not expose the flag: a flag that is accepted but never used
// (like the removed -o on `vm cloud-init set`/`regenerate`) is exactly the
// contract violation this test guards against. Keep the list in sync when a
// command gains or loses structured output.
var structuredOutputCommands = map[string]bool{
	"status":             true,
	"check":              true,
	"doctor":             true,
	"version":            true,
	"guest ls":           true,
	"guest get":          true,
	"guest start":        true,
	"guest shutdown":     true,
	"guest reboot":       true,
	"guest stop":         true,
	"vm ls":              true,
	"vm get":             true,
	"vm agent network":   true,
	"vm agent exec":      true,
	"vm cloud-init get":  true,
	"vm clone":           true,
	"vm snapshot ls":     true,
	"vm backup":          true,
	"vm restore":         true,
	"lxc ls":             true,
	"lxc get":            true,
	"lxc clone":          true,
	"lxc snapshot ls":    true,
	"lxc backup":         true,
	"lxc restore":        true,
	"node ls":            true,
	"node get":           true,
	"task ls":            true,
	"task get":           true,
	"task log":           true,
	"task wait":          true,
	"storage ls":         true,
	"storage usage":      true,
	"storage get":        true,
	"storage content ls": true,
	"backup ls":          true,
	"network ls":         true,
	"network get":        true,
	"firewall status":    true,
	"firewall ls":        true,
}

// TestOutputFlagOnlyOnStructuredCommands walks the whole command tree and
// asserts that exactly the allowlisted commands expose -o/--output.
func TestOutputFlagOnlyOnStructuredCommands(t *testing.T) {
	app := NewAppWithDependencies("test", Dependencies{})
	seen := make(map[string]bool)
	var walk func(path []string, commands []*cli.Command)
	walk = func(path []string, commands []*cli.Command) {
		for _, command := range commands {
			leaf := append(append([]string{}, path...), command.Name)
			if len(command.Subcommands) > 0 {
				walk(leaf, command.Subcommands)
				continue
			}
			name := strings.Join(leaf, " ")
			seen[name] = true
			hasOutput := false
			for _, flag := range command.Flags {
				for _, flagName := range flag.Names() {
					if flagName == "output" || flagName == "o" {
						hasOutput = true
					}
				}
			}
			if hasOutput != structuredOutputCommands[name] {
				t.Errorf("%s: exposes -o/--output = %v, want %v", name, hasOutput, structuredOutputCommands[name])
			}
		}
	}
	walk(nil, app.Commands)

	for name := range structuredOutputCommands {
		if !seen[name] {
			t.Errorf("allowlisted command %q no longer exists; update the allowlist after renames", name)
		}
	}
}
