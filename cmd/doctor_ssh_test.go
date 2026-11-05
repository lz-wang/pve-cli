package cmd

import (
	"bytes"
	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/lz-wang/pve-cli/v3/internal/ssh"
	"strings"
	"testing"
)

func TestDoctorSSHStructuredMissingConfiguration(t *testing.T) {
	for _, format := range []string{"json", "yaml", "table"} {
		var out bytes.Buffer
		err := RunWithDependencies([]string{"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150", "doctor", "--ssh", "-o", format}, "test", Dependencies{Stdout: &out, Stderr: &bytes.Buffer{}, BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) {
			return &commandBackend{nodes: []output.NodeRow{{Name: "n"}}}, nil
		}})
		if err == nil || !strings.Contains(out.String(), "SSH_CONFIG") || !strings.Contains(out.String(), "API_CONNECTIVITY") {
			t.Fatalf("%v %s", err, out.String())
		}
	}
}
func TestDoctorSSHOfflineDoesNotConnect(t *testing.T) {
	var out bytes.Buffer
	err := RunWithDependencies([]string{"pve", "--config", writeTwoProfileConfig(t), "doctor", "--offline", "--ssh", "-o", "json"}, "test", Dependencies{Stdout: &out, Stderr: &bytes.Buffer{}, BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) {
		t.Fatal("offline API connection")
		return nil, nil
	}, SSHRunnerFactory: func(ssh.Profile) pve.CommandRunner { t.Fatal("offline SSH connection"); return nil }})
	if err != nil || !strings.Contains(out.String(), "offline mode does not connect through SSH") {
		t.Fatalf("%v %s", err, out.String())
	}
}
