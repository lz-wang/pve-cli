// Command docgen keeps the command and output references synchronized with Go definitions.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/lz-wang/pve-cli/v3/cmd"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/urfave/cli/v2"
)

const marker = "<!-- GENERATED_REFERENCE -->"

func cell(s string) string {
	return strings.ReplaceAll(strings.Join(strings.Fields(s), " "), "|", "\\|")
}
func flags(b *bytes.Buffer, items []cli.Flag) {
	if len(items) == 0 {
		return
	}
	b.WriteString("\n| Flag | Description and default |\n|---|---|\n")
	for _, f := range items {
		names := []string{}
		for _, n := range f.Names() {
			prefix := "--"
			if len(n) == 1 {
				prefix = "-"
			}
			names = append(names, "`"+prefix+n+"`")
		}
		required := ""
		if r, ok := f.(cli.RequiredFlag); ok && r.IsRequired() {
			required = " **Required.**"
		}
		fmt.Fprintf(b, "| %s | %s%s |\n", strings.Join(names, ", "), cell(f.String()), required)
	}
}
func commands(b *bytes.Buffer, path string, items []*cli.Command) {
	for _, c := range items {
		if c.Hidden {
			continue
		}
		name := strings.TrimSpace(path + " " + c.Name)
		if len(c.Subcommands) > 0 {
			commands(b, name, c.Subcommands)
			continue
		}
		fmt.Fprintf(b, "\n### `%s`\n\n%s\n\n```text\n%s\n```\n", name, c.Usage, strings.TrimSpace(name+" "+c.ArgsUsage))
		flags(b, c.Flags)
	}
}
func fields(b *bytes.Buffer, t reflect.Type) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			fields(b, f.Type)
			continue
		}
		tag := f.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "-" || name == "" {
			continue
		}
		optional := "no"
		if strings.Contains(tag, "omitempty") {
			optional = "yes"
		}
		typ := strings.TrimPrefix(f.Type.String(), "output.")
		fmt.Fprintf(b, "| `%s` | `%s` | %s |\n", name, typ, optional)
	}
}
func update(path string, generated string, check bool) error {
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	intro, _, found := strings.Cut(string(old), marker)
	if !found {
		return fmt.Errorf("missing generation marker in %s", path)
	}
	wanted := []byte(intro + marker + "\n" + generated)
	if bytes.Equal(old, wanted) {
		return nil
	}
	if check {
		return fmt.Errorf("%s is stale; run make docs", path)
	}
	return os.WriteFile(path, wanted, 0644)
}
func run() error {
	check := flag.Bool("check", false, "verify generated references")
	flag.Parse()
	app := cmd.NewApp("v3.0.0")
	var usage bytes.Buffer
	usage.WriteString("\n## Global flags\n\nPlace global flags before the resource command. `--help` is available on every command.\n")
	flags(&usage, app.Flags)
	usage.WriteString("\n## Command reference\n\nGenerated from the registered command tree; flags may follow positional IDs.\n")
	commands(&usage, "pve", app.Commands)
	if err := update("docs/usage.md", usage.String(), *check); err != nil {
		return err
	}
	var schema bytes.Buffer
	types := []any{output.NodeRow{}, output.NodeDetail{}, output.GuestRow{}, output.OperationResult{}, output.CloneResult{}, output.SnapshotRow{}, output.SnapshotPlan{}, output.BackupRow{}, output.BackupResult{}, output.RestoreResult{}, output.StatusReport{}, output.NodeSummary{}, output.GuestSummary{}, output.StorageSummary{}, output.BackupSummary{}, output.StatusIssue{}, output.BulkGuestResult{}, output.AgentNetworkRow{}, output.AgentExecResult{}, output.CloudInitIPConfig{}, output.CloudInitCustom{}, output.CloudInitConfig{}, output.NetworkRow{}, output.FirewallStatusRow{}, output.FirewallRuleRow{}, output.StorageRow{}, output.StorageContentRow{}, output.DoctorRow{}, output.CheckRow{}, output.TaskRow{}, output.TaskLogRow{}, output.VersionInfo{}, output.GuestConfig{}, output.TemplateRow{}, output.DiskRow{}, output.DiskResult{}}
	for _, value := range types {
		t := reflect.TypeOf(value)
		fmt.Fprintf(&schema, "\n## %s\n\n| Field | Go / wire value type | May be omitted |\n|---|---|---|\n", t.Name())
		fields(&schema, t)
	}
	return update("docs/output-schema.md", schema.String(), *check)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
