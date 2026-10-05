package cmd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"
)

func testNormalize(t *testing.T, args []string) []string {
	t.Helper()
	return normalizeArgs(NewAppWithDependencies("test", Dependencies{}), args)
}

func TestNormalizeArgsMovesLeafFlagsBeforePositionals(t *testing.T) {
	args := []string{"pve", "vm", "start", "100", "--wait", "--wait-timeout", "10s"}
	want := []string{"pve", "vm", "start", "--wait", "--wait-timeout", "10s", "100"}

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesRebootFlagsBeforePositionals(t *testing.T) {
	args := []string{"pve", "vm", "reboot", "101", "--wait"}
	want := []string{"pve", "vm", "reboot", "--wait", "101"}

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesOutputFlag(t *testing.T) {
	args := []string{"pve", "vm", "get", "100", "-o", "json"}
	want := []string{"pve", "vm", "get", "-o", "json", "100"}

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesGuestTypeAndOutputFlags(t *testing.T) {
	args := []string{"pve", "guest", "get", "100", "--type", "vm", "-o", "json"}
	want := []string{"pve", "guest", "get", "--type", "vm", "-o", "json", "100"}

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesRepeatedSetFlags(t *testing.T) {
	args := []string{"pve", "vm", "config", "101", "--set", "memory=4096", "--set", "cores=4", "--wait"}
	want := []string{"pve", "vm", "config", "--set", "memory=4096", "--set", "cores=4", "--wait", "101"}

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesResizeFlags(t *testing.T) {
	args := []string{"pve", "vm", "resize", "101", "--disk", "scsi0", "--size", "+20G", "--wait"}
	want := []string{"pve", "vm", "resize", "--disk", "scsi0", "--size", "+20G", "--wait", "101"}

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesNestedSnapshotFlags(t *testing.T) {
	args := []string{"pve", "vm", "snapshot", "create", "101", "before-upgrade", "--wait"}
	want := []string{"pve", "vm", "snapshot", "create", "--wait", "101", "before-upgrade"}

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesBackupListFlags(t *testing.T) {
	args := []string{"pve", "backup", "ls", "--node", "pve1", "--storage", "backup", "--type", "vm", "--latest"}
	want := []string{"pve", "backup", "ls", "--node", "pve1", "--storage", "backup", "--type", "vm", "--latest"}

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesGuestBackupFlags(t *testing.T) {
	args := []string{"pve", "vm", "backup", "100", "--storage", "backup", "--mode", "stop", "--compress", "none", "--protected", "--wait"}
	want := []string{"pve", "vm", "backup", "--storage", "backup", "--mode", "stop", "--compress", "none", "--protected", "--wait", "100"}

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesStorageGetFlags(t *testing.T) {
	args := []string{"pve", "storage", "get", "local", "--node", "pve1", "-o", "json"}
	want := []string{"pve", "storage", "get", "--node", "pve1", "-o", "json", "local"}

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesStorageContentFlags(t *testing.T) {
	args := []string{"pve", "storage", "content", "ls", "--node", "pve1", "--storage", "local", "--content", "backup", "--vmid", "100"}
	want := []string{"pve", "storage", "content", "ls", "--node", "pve1", "--storage", "local", "--content", "backup", "--vmid", "100"}

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesStorageContentPositionalAfterDeepPath(t *testing.T) {
	args := []string{"pve", "vm", "cloud-init", "set", "100", "--user", "debian", "--ipconfig0", "ip=dhcp"}
	want := []string{"pve", "vm", "cloud-init", "set", "--user", "debian", "--ipconfig0", "ip=dhcp", "100"}

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsKeepsAgentExecCommandVerbatimAfterSeparator(t *testing.T) {
	args := []string{"pve", "vm", "agent", "exec", "100", "--timeout", "5m", "--", "/usr/bin/uname", "-a"}
	want := []string{"pve", "vm", "agent", "exec", "--timeout", "5m", "100", "--", "/usr/bin/uname", "-a"}

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsKeepsGlobalFlagsBeforeRootCommand(t *testing.T) {
	args := []string{"pve", "--config", "/tmp/pve.yaml", "--profile", "lab", "--api-timeout", "10s", "vm", "get", "100", "-o", "json"}
	want := []string{"pve", "--config", "/tmp/pve.yaml", "--profile", "lab", "--api-timeout", "10s", "vm", "get", "-o", "json", "100"}

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsKeepsBulkValueFlagsAttached(t *testing.T) {
	args := []string{"pve", "guest", "shutdown", "--node", "pve1", "--status", "running", "--tag", "infra", "--jobs", "4", "--dry-run"}
	want := append([]string{}, args...)

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsKeepsCheckValueFlagsAttached(t *testing.T) {
	args := []string{"pve", "check", "--node", "pve1", "--storage-warn", "80", "--storage-fail", "95", "--backup-tag", "backup", "--backup-max-age", "36h"}
	want := append([]string{}, args...)

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesValueFlagBeforePositional(t *testing.T) {
	args := []string{"pve", "vm", "backup", "100", "--storage", "backup"}
	want := []string{"pve", "vm", "backup", "--storage", "backup", "100"}

	if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsLeavesUnknownCommandsAndShortArgsAlone(t *testing.T) {
	for _, args := range [][]string{
		{"pve", "nonsense", "100", "--wait"},
		{"pve", "--", "vm", "start"},
		{"pve"},
		{"pve", "vm"},
		{"pve", "vm", "start"},
	} {
		want := append([]string{}, args...)
		if got := testNormalize(t, args); !reflect.DeepEqual(got, want) {
			t.Fatalf("normalize(%#v) = %#v, want %#v", args, got, want)
		}
	}
}

// TestNormalizeArgsDerivesValueFlagsFromCommandTree guards the single source
// of truth: the flags of every leaf command in the tree must round-trip a
// positional after them, which only works when value-taking flags are derived
// from the cli.Flag definitions themselves.
func TestNormalizeArgsDerivesValueFlagsFromCommandTree(t *testing.T) {
	app := NewAppWithDependencies("test", Dependencies{})
	var walk func(path []*cli.Command, commands []*cli.Command)
	walk = func(path []*cli.Command, commands []*cli.Command) {
		for _, command := range commands {
			if len(command.Subcommands) > 0 {
				walk(append(path, command), command.Subcommands)
				continue
			}
			var args []string = []string{"pve"}
			for _, segment := range path {
				args = append(args, segment.Name)
			}
			args = append(args, command.Name, "positional")
			for _, flag := range command.Flags {
				if !flagTakesValue(flag) {
					continue
				}
				for _, name := range flag.Names() {
					token := "--" + name
					if len(name) == 1 {
						token = "-" + name
					}
					leaf := append(append([]string{}, args...), token, "value")
					got := normalizeArgs(app, leaf)
					if got[len(got)-3] != token || got[len(got)-2] != "value" || got[len(got)-1] != "positional" {
						t.Fatalf("%s: value flag %s did not stay attached to its value with the positional last: %#v", strings.Join(leaf[1:], " "), token, got)
					}
				}
			}
		}
	}
	walk(nil, app.Commands)
}
