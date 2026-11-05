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

type auditCommandBackend struct{ *commandBackend }

func (b auditCommandBackend) GuestConfig(context.Context, string, string, int) (map[string]any, error) {
	return map[string]any{"mp0": "/share,mp=/data,backup=0"}, nil
}
func TestBackupAuditCLI(t *testing.T) {
	for _, format := range []string{"json", "yaml", "table"} {
		t.Run(format, func(t *testing.T) {
			var stdout bytes.Buffer
			backend := auditCommandBackend{&commandBackend{nodes: []output.NodeRow{{Name: "n"}}, lxcs: map[string][]output.GuestRow{"n": {{Kind: "lxc", VMID: 200, Node: "n", Name: "files"}}}}}
			err := RunWithDependencies([]string{"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150", "backup", "audit", "--strict", "-o", format}, "test", Dependencies{Stdout: &stdout, Stderr: &bytes.Buffer{}, BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) { return backend, nil }})
			if err == nil || !strings.Contains(stdout.String(), "missing") || !strings.Contains(stdout.String(), "outside archive") {
				t.Fatalf("%v %s", err, stdout.String())
			}
		})
	}
}
