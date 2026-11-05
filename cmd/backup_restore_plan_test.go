package cmd

import (
	"bytes"
	"context"
	"errors"
	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"strings"
	"testing"
)

type archiveCommandBackend struct{ *commandBackend }

func (b archiveCommandBackend) ArchiveConfig(context.Context, string, string) (map[string]any, error) {
	return map[string]any{"rootfs": "missing:x,size=64G", "password": "NOT-FOR-OUTPUT"}, nil
}
func TestBackupRestorePlanCLI(t *testing.T) {
	for _, format := range []string{"json", "yaml", "table"} {
		t.Run(format, func(t *testing.T) {
			var out bytes.Buffer
			archive := "local:backup/vzdump-lxc-100-test.tar.zst"
			b := archiveCommandBackend{&commandBackend{nodes: []output.NodeRow{{Name: "n"}}, backups: map[string]map[string][]output.BackupRow{"n": {"local": {{VolID: archive, Kind: "lxc", VMID: 100}}}}}}
			err := RunWithDependencies([]string{"pve", "--config", writeTwoProfileConfig(t), "backup", "restore-plan", archive, "--from", "n150", "--to", "pve8845", "--node", "n", "--target-node", "n", "--target-vmid", "900", "--type", "lxc", "-o", format}, "test", Dependencies{Stdout: &out, Stderr: &bytes.Buffer{}, BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) { return b, nil }})
			if err == nil || !strings.Contains(out.String(), "disk.rootfs") || strings.Contains(out.String(), "NOT-FOR-OUTPUT") {
				t.Fatalf("%v %s", err, out.String())
			}
		})
	}
	if _, err := parseUniqueMapping([]string{"a=b", "a=c"}); err == nil {
		t.Fatal("duplicate mapping accepted")
	}
}

func TestBackupRestorePlanUnavailableInventory(t *testing.T) {
	archive := "local:backup/vzdump-lxc-100-test.tar.zst"
	b := archiveCommandBackend{&commandBackend{nodes: []output.NodeRow{{Name: "n"}}, backups: map[string]map[string][]output.BackupRow{"n": {"local": {{VolID: archive, Kind: "lxc", VMID: 100}}}}, storageErrs: map[string]error{"n": errors.New("unavailable")}}}
	var out bytes.Buffer
	err := RunWithDependencies([]string{"pve", "--config", writeTwoProfileConfig(t), "backup", "restore-plan", archive, "--from", "n150", "--to", "pve8845", "--node", "n", "--target-node", "n", "--target-vmid", "900", "--type", "lxc", "-o", "json"}, "test", Dependencies{Stdout: &out, Stderr: &bytes.Buffer{}, BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) { return b, nil }})
	if err == nil || !strings.Contains(err.Error(), "inventory incomplete") || !strings.Contains(out.String(), "unknown") {
		t.Fatalf("%v %s", err, out.String())
	}
}
