package cmd

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/urfave/cli/v2"
)

func TestNormalizeArgsMovesLeafFlagsBeforePositionals(t *testing.T) {
	args := []string{"pve", "vm", "start", "100", "--wait", "--wait-timeout", "10s"}
	want := []string{"pve", "vm", "start", "--wait", "--wait-timeout", "10s", "100"}

	if got := normalizeArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesRebootFlagsBeforePositionals(t *testing.T) {
	args := []string{"pve", "vm", "reboot", "101", "--wait"}
	want := []string{"pve", "vm", "reboot", "--wait", "101"}

	if got := normalizeArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesOutputFlag(t *testing.T) {
	args := []string{"pve", "vm", "get", "100", "-o", "json"}
	want := []string{"pve", "vm", "get", "-o", "json", "100"}

	if got := normalizeArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesGuestTypeAndOutputFlags(t *testing.T) {
	args := []string{"pve", "guest", "get", "100", "--type", "vm", "-o", "json"}
	want := []string{"pve", "guest", "get", "--type", "vm", "-o", "json", "100"}

	if got := normalizeArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesRepeatedSetFlags(t *testing.T) {
	args := []string{"pve", "vm", "config", "101", "--set", "memory=4096", "--set", "cores=4", "--wait"}
	want := []string{"pve", "vm", "config", "--set", "memory=4096", "--set", "cores=4", "--wait", "101"}

	if got := normalizeArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesResizeFlags(t *testing.T) {
	args := []string{"pve", "vm", "resize", "101", "--disk", "scsi0", "--size", "+20G", "--wait"}
	want := []string{"pve", "vm", "resize", "--disk", "scsi0", "--size", "+20G", "--wait", "101"}

	if got := normalizeArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesNestedSnapshotFlags(t *testing.T) {
	args := []string{"pve", "vm", "snapshot", "create", "101", "before-upgrade", "--wait"}
	want := []string{"pve", "vm", "snapshot", "create", "--wait", "101", "before-upgrade"}

	if got := normalizeArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesBackupListFlags(t *testing.T) {
	args := []string{"pve", "backup", "ls", "--node", "pve1", "--storage", "backup", "--kind", "vm", "--latest"}
	want := []string{"pve", "backup", "ls", "--node", "pve1", "--storage", "backup", "--kind", "vm", "--latest"}

	if got := normalizeArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesGuestBackupFlags(t *testing.T) {
	args := []string{"pve", "vm", "backup", "100", "--storage", "backup", "--mode", "stop", "--compress", "none", "--protected", "1", "--wait"}
	want := []string{"pve", "vm", "backup", "--storage", "backup", "--mode", "stop", "--compress", "none", "--protected", "1", "--wait", "100"}

	if got := normalizeArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesStorageGetFlags(t *testing.T) {
	args := []string{"pve", "storage", "get", "local", "--node", "pve1", "-o", "json"}
	want := []string{"pve", "storage", "get", "--node", "pve1", "-o", "json", "local"}

	if got := normalizeArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesStorageContentFlags(t *testing.T) {
	args := []string{"pve", "storage", "content", "ls", "--node", "pve1", "--storage", "local", "--content", "backup", "--vmid", "100"}
	want := []string{"pve", "storage", "content", "ls", "--node", "pve1", "--storage", "local", "--content", "backup", "--vmid", "100"}

	if got := normalizeArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsKeepsBulkValueFlagsAttached(t *testing.T) {
	args := []string{"pve", "guest", "shutdown", "--node", "pve1", "--status", "running", "--tag", "infra", "--jobs", "4", "--dry-run"}
	want := append([]string{}, args...)

	if got := normalizeArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsKeepsCheckValueFlagsAttached(t *testing.T) {
	args := []string{"pve", "check", "--node", "pve1", "--storage-warn", "80", "--storage-fail", "95", "--backup-tag", "backup", "--backup-max-age", "36h"}
	want := append([]string{}, args...)

	if got := normalizeArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

func TestNormalizeArgsMovesValueFlagBeforePositional(t *testing.T) {
	args := []string{"pve", "guest", "get", "100", "--status", "running"}
	want := []string{"pve", "guest", "get", "--status", "running", "100"}

	if got := normalizeArgs(args); !reflect.DeepEqual(got, want) {
		t.Fatalf("normalize = %#v, want %#v", got, want)
	}
}

// TestFlagsWithValuesCoversAllValueFlags guards against normalize.go and the
// urfave/cli flag definitions drifting apart: every value-taking flag in the
// command tree must be listed in flagsWithValues, or its value would be
// reordered as a positional argument.
func TestFlagsWithValuesCoversAllValueFlags(t *testing.T) {
	app := NewAppWithDependencies("test", Dependencies{})

	var missing []string
	var walkFlags func(path string, flags []cli.Flag)
	walkFlags = func(path string, flags []cli.Flag) {
		for _, flag := range flags {
			if !flagTakesValue(flag) {
				continue
			}
			for _, name := range flag.Names() {
				if flagsWithValues["--"+name] || flagsWithValues["-"+name] {
					continue
				}
				missing = append(missing, fmt.Sprintf("%s%s", path, name))
			}
		}
	}
	var walkCommands func(path string, commands []*cli.Command)
	walkCommands = func(path string, commands []*cli.Command) {
		for _, command := range commands {
			child := path + command.Name + " "
			walkFlags(child, command.Flags)
			walkCommands(child, command.Subcommands)
		}
	}

	walkFlags("pve ", app.Flags)
	walkCommands("", app.Commands)

	if len(missing) > 0 {
		t.Fatalf("value flags missing from flagsWithValues: %v", missing)
	}
}

// flagTakesValue reports whether a flag consumes the following CLI token.
// Unknown flag types are treated as value-taking so future flag kinds fail
// the meta-test instead of silently skipping normalization.
func flagTakesValue(flag cli.Flag) bool {
	switch flag.(type) {
	case *cli.BoolFlag:
		return false
	default:
		return true
	}
}
