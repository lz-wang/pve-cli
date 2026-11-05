package cmd

import (
	"bytes"
	"context"
	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"strings"
	"testing"
)

type deleteCommandBackend struct {
	*commandBackend
	deleted bool
}

func (b *deleteCommandBackend) DeleteBackup(context.Context, string, string) (pve.Task, error) {
	b.deleted = true
	return nil, nil
}
func (b *deleteCommandBackend) Backups(context.Context, string, string) ([]output.BackupRow, error) {
	if b.deleted {
		return []output.BackupRow{}, nil
	}
	return []output.BackupRow{{VolID: "local:backup/vzdump-lxc-900-test.tar.zst", Kind: "lxc", VMID: 900}}, nil
}
func TestBackupDeleteConfirmationAndOutput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		flags   []string
		input   string
		deleted bool
		wantErr bool
	}{
		{"dry run", []string{"--dry-run"}, "", false, false}, {"refuse", nil, "wrong\n", false, true}, {"confirm", nil, "local:backup/vzdump-lxc-900-test.tar.zst\n", true, false}, {"yes", []string{"--yes"}, "", true, false},
	} {
		for _, format := range []string{"json", "yaml", "table"} {
			t.Run(tc.name+format, func(t *testing.T) {
				b := &deleteCommandBackend{commandBackend: &commandBackend{}}
				var out bytes.Buffer
				args := []string{"pve", "--config", writeTwoProfileConfig(t), "backup", "delete", "local:backup/vzdump-lxc-900-test.tar.zst", "--node", "n", "-o", format}
				args = append(args, tc.flags...)
				err := RunWithDependencies(args, "test", Dependencies{Stdout: &out, Stderr: &bytes.Buffer{}, Stdin: strings.NewReader(tc.input), BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) { return b, nil }})
				if (err != nil) != tc.wantErr || b.deleted != tc.deleted {
					t.Fatalf("%v deleted=%v %s", err, b.deleted, out.String())
				}
				if !tc.wantErr && !strings.Contains(out.String(), "vzdump-lxc-900-test") {
					t.Fatal(out.String())
				}
			})
		}
	}
}
