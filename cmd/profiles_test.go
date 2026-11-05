package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProfileQueriesPreservePartialResults(t *testing.T) {
	for _, format := range []string{"json", "yaml", "table"} {
		t.Run(format, func(t *testing.T) {
			var out bytes.Buffer
			var calls atomic.Int32
			err := RunWithDependencies([]string{"pve", "--config", writeTwoProfileConfig(t), "guest", "ls", "--all-profiles", "--workers", "2", "-o", format}, "test", Dependencies{Stdout: &out, Stderr: &bytes.Buffer{}, BackendFactory: func(profile config.Profile, _ pve.ClientOptions) (pve.Backend, error) {
				calls.Add(1)
				if strings.Contains(profile.Endpoint, "pve8845") {
					return nil, errors.New("offline")
				}
				return &commandBackend{nodes: []output.NodeRow{{Name: "n150"}}, vms: map[string][]output.GuestRow{"n150": {{Kind: "vm", VMID: 100, Node: "n150", Name: "test"}}}}, nil
			}})
			if err == nil || calls.Load() != 2 {
				t.Fatalf("err=%v calls=%d", err, calls.Load())
			}
			if !strings.Contains(out.String(), "n150") || !strings.Contains(out.String(), "offline") {
				t.Fatal(out.String())
			}
			if format == "json" {
				var r output.ProfileReport
				if e := json.Unmarshal(out.Bytes(), &r); e != nil {
					t.Fatal(e)
				}
				if r.Complete || len(r.Results) != 2 || !r.Results[0].Complete || r.Results[1].Complete {
					t.Fatalf("%+v", r)
				}
				rows := r.Results[0].Data.([]any)
				if rows[0].(map[string]any)["vmid"] != float64(100) {
					t.Fatal(rows)
				}
			}
		})
	}
}
func TestProfileSelectionRejectedBeforeConnections(t *testing.T) {
	for _, args := range [][]string{
		{"status", "--all-profiles", "--profiles", "n150"},
		{"--profile", "n150", "status", "--all-profiles"},
		{"status", "--profiles", "missing"},
		{"status", "--all-profiles", "--workers", "9"},
		{"guest", "start", "--all-profiles"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			err := RunWithDependencies(append([]string{"pve", "--config", writeTwoProfileConfig(t)}, args...), "test", Dependencies{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) {
				t.Fatal("unexpected connection")
				return nil, nil
			}})
			if ExitCode(err) != 2 {
				t.Fatalf("%v exit=%d", err, ExitCode(err))
			}
		})
	}
}
func TestProfileQueriesRespectFiltersAndDeduplicate(t *testing.T) {
	var out bytes.Buffer
	var calls atomic.Int32
	err := RunWithDependencies([]string{"pve", "--config", writeTwoProfileConfig(t), "guest", "ls", "--profiles", "n150,n150", "--type", "vm", "--name", "keep", "-o", "json"}, "test", Dependencies{Stdout: &out, Stderr: &bytes.Buffer{}, BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) {
		calls.Add(1)
		return &commandBackend{nodes: []output.NodeRow{{Name: "n150"}}, vms: map[string][]output.GuestRow{"n150": {{Kind: "vm", VMID: 100, Name: "keep"}, {Kind: "vm", VMID: 101, Name: "drop"}}}}, nil
	}})
	if err != nil || calls.Load() != 1 || strings.Contains(out.String(), "drop") || !strings.Contains(out.String(), "keep") {
		t.Fatalf("%v %d %s", err, calls.Load(), out.String())
	}
}
