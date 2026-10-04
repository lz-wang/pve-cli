package pve

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lz-wang/pvectl/internal/config"
	"github.com/lz-wang/pvectl/internal/output"
)

func TestDoctorMissingConfigFile(t *testing.T) {
	result := NewDoctorService(nil).Run(context.Background(), DoctorOptions{
		ConfigPath: filepath.Join(t.TempDir(), "missing.yaml"),
		Offline:    true,
	})

	requireDoctorRow(t, result, "CONFIG_FILE", output.DoctorStatusFail)
	requireDoctorRow(t, result, "CONFIG_PARSE", output.DoctorStatusSkip)
	if !result.Failed {
		t.Fatal("expected failed result")
	}
}

func TestDoctorInvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("profiles: ["), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	result := NewDoctorService(nil).Run(context.Background(), DoctorOptions{ConfigPath: path, Offline: true})

	row := requireDoctorRow(t, result, "CONFIG_PARSE", output.DoctorStatusFail)
	if !strings.Contains(row.Message, "parse config") {
		t.Fatalf("parse message = %q", row.Message)
	}
}

func TestDoctorMissingCurrentProfile(t *testing.T) {
	path := writeDoctorConfig(t, &config.Config{
		Profiles: map[string]config.Profile{"home": validDoctorProfile()},
	})

	result := NewDoctorService(nil).Run(context.Background(), DoctorOptions{ConfigPath: path, Offline: true})

	requireDoctorRow(t, result, "CURRENT_PROFILE", output.DoctorStatusFail)
	requireDoctorRow(t, result, "PROFILE_FIELDS", output.DoctorStatusSkip)
}

func TestDoctorProfileNotFound(t *testing.T) {
	path := writeDoctorConfig(t, &config.Config{
		CurrentProfile: "missing",
		Profiles:       map[string]config.Profile{"home": validDoctorProfile()},
	})

	result := NewDoctorService(nil).Run(context.Background(), DoctorOptions{ConfigPath: path, Offline: true})

	row := requireDoctorRow(t, result, "CURRENT_PROFILE", output.DoctorStatusFail)
	if !strings.Contains(row.Message, `profile "missing" not found`) {
		t.Fatalf("profile message = %q", row.Message)
	}
}

func TestDoctorMissingEndpoint(t *testing.T) {
	t.Setenv("PVECTL_DOCTOR_TOKEN", "secret")
	profile := validDoctorProfile()
	profile.Endpoint = ""
	path := writeDoctorProfile(t, profile)

	result := NewDoctorService(nil).Run(context.Background(), DoctorOptions{ConfigPath: path, Offline: true})

	row := requireDoctorRow(t, result, "PROFILE_FIELDS", output.DoctorStatusFail)
	if !strings.Contains(row.Message, "endpoint") {
		t.Fatalf("profile fields message = %q", row.Message)
	}
	requireDoctorRow(t, result, "ENDPOINT", output.DoctorStatusFail)
}

func TestDoctorMissingTokenEnv(t *testing.T) {
	profile := validDoctorProfile()
	profile.TokenSecretEnv = ""
	path := writeDoctorProfile(t, profile)

	result := NewDoctorService(nil).Run(context.Background(), DoctorOptions{ConfigPath: path, Offline: true})

	requireDoctorRow(t, result, "PROFILE_FIELDS", output.DoctorStatusFail)
	requireDoctorRow(t, result, "TOKEN_SECRET_ENV", output.DoctorStatusFail)
}

func TestDoctorTokenEnvEmpty(t *testing.T) {
	profile := validDoctorProfile()
	profile.TokenSecretEnv = "PVECTL_DOCTOR_EMPTY_TOKEN"
	path := writeDoctorProfile(t, profile)

	result := NewDoctorService(nil).Run(context.Background(), DoctorOptions{ConfigPath: path, Offline: true})

	row := requireDoctorRow(t, result, "TOKEN_SECRET_ENV", output.DoctorStatusFail)
	if !strings.Contains(row.Message, "PVECTL_DOCTOR_EMPTY_TOKEN") {
		t.Fatalf("token env message = %q", row.Message)
	}
}

func TestDoctorInvalidTimeout(t *testing.T) {
	t.Setenv("PVECTL_DOCTOR_TOKEN", "secret")
	profile := validDoctorProfile()
	profile.Timeout = "soon"
	path := writeDoctorProfile(t, profile)

	result := NewDoctorService(nil).Run(context.Background(), DoctorOptions{ConfigPath: path, Offline: true})

	requireDoctorRow(t, result, "TIMEOUT", output.DoctorStatusFail)
}

func TestDoctorInvalidOutput(t *testing.T) {
	t.Setenv("PVECTL_DOCTOR_TOKEN", "secret")
	profile := validDoctorProfile()
	profile.DefaultOutput = "xml"
	path := writeDoctorProfile(t, profile)

	result := NewDoctorService(nil).Run(context.Background(), DoctorOptions{ConfigPath: path, Offline: true})

	requireDoctorRow(t, result, "DEFAULT_OUTPUT", output.DoctorStatusFail)
	if result.Format != output.FormatTable {
		t.Fatalf("format = %q, want table", result.Format)
	}
}

func TestDoctorInvalidEndpointURL(t *testing.T) {
	t.Setenv("PVECTL_DOCTOR_TOKEN", "secret")
	profile := validDoctorProfile()
	profile.Endpoint = "ftp://pve.lan/api2/json"
	path := writeDoctorProfile(t, profile)

	result := NewDoctorService(nil).Run(context.Background(), DoctorOptions{ConfigPath: path, Offline: true})

	requireDoctorRow(t, result, "ENDPOINT", output.DoctorStatusFail)
}

func TestDoctorOfflineSkipsAPI(t *testing.T) {
	t.Setenv("PVECTL_DOCTOR_TOKEN", "secret")
	path := writeDoctorProfile(t, validDoctorProfile())
	called := false
	service := NewDoctorService(func(config.Profile, ClientOptions) (Backend, error) {
		called = true
		return doctorBackend{}, nil
	})

	result := service.Run(context.Background(), DoctorOptions{ConfigPath: path, Offline: true})

	requireDoctorRow(t, result, "API_CONNECTIVITY", output.DoctorStatusSkip)
	requireDoctorRow(t, result, "NODES", output.DoctorStatusSkip)
	if called {
		t.Fatal("backend factory should not be called in offline mode")
	}
	if result.Failed {
		t.Fatal("offline valid config should not fail")
	}
}

func TestDoctorOnlineNodesOK(t *testing.T) {
	t.Setenv("PVECTL_DOCTOR_TOKEN", "secret")
	path := writeDoctorProfile(t, validDoctorProfile())
	service := NewDoctorService(doctorFactory(doctorBackend{
		nodes: []output.NodeRow{{Name: "pve1"}, {Name: "pve2"}},
	}, nil))

	result := service.Run(context.Background(), DoctorOptions{ConfigPath: path})

	requireDoctorRow(t, result, "API_CONNECTIVITY", output.DoctorStatusOK)
	row := requireDoctorRow(t, result, "NODES", output.DoctorStatusOK)
	if row.Message != "2 node(s)" {
		t.Fatalf("nodes message = %q", row.Message)
	}
	if result.Failed {
		t.Fatal("valid online doctor should not fail")
	}
}

func TestDoctorOnlineNodesFail(t *testing.T) {
	t.Setenv("PVECTL_DOCTOR_TOKEN", "secret")
	path := writeDoctorProfile(t, validDoctorProfile())
	service := NewDoctorService(doctorFactory(doctorBackend{
		nodesErr: errors.New("permission denied"),
	}, nil))

	result := service.Run(context.Background(), DoctorOptions{ConfigPath: path})

	row := requireDoctorRow(t, result, "API_CONNECTIVITY", output.DoctorStatusFail)
	if !strings.Contains(row.Message, "permission denied") {
		t.Fatalf("api message = %q", row.Message)
	}
	requireDoctorRow(t, result, "NODES", output.DoctorStatusSkip)
}

func TestDoctorNodeExists(t *testing.T) {
	t.Setenv("PVECTL_DOCTOR_TOKEN", "secret")
	path := writeDoctorProfile(t, validDoctorProfile())
	service := NewDoctorService(doctorFactory(doctorBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
	}, nil))

	result := service.Run(context.Background(), DoctorOptions{ConfigPath: path, Node: "pve1"})

	requireDoctorRow(t, result, "NODE", output.DoctorStatusOK)
}

func TestDoctorNodeMissing(t *testing.T) {
	t.Setenv("PVECTL_DOCTOR_TOKEN", "secret")
	path := writeDoctorProfile(t, validDoctorProfile())
	service := NewDoctorService(doctorFactory(doctorBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
	}, nil))

	result := service.Run(context.Background(), DoctorOptions{ConfigPath: path, Node: "pve2"})

	requireDoctorRow(t, result, "NODE", output.DoctorStatusFail)
	if !result.Failed {
		t.Fatal("missing node should fail")
	}
}

func requireDoctorRow(t *testing.T, result DoctorResult, check string, status output.DoctorStatus) output.DoctorRow {
	t.Helper()
	for _, row := range result.Rows {
		if row.Check == check {
			if row.Status != status {
				t.Fatalf("%s status = %q, want %q; rows = %#v", check, row.Status, status, result.Rows)
			}
			return row
		}
	}
	t.Fatalf("missing doctor row %s; rows = %#v", check, result.Rows)
	return output.DoctorRow{}
}

func writeDoctorProfile(t *testing.T, profile config.Profile) string {
	t.Helper()
	return writeDoctorConfig(t, &config.Config{
		CurrentProfile: "home",
		Profiles:       map[string]config.Profile{"home": profile},
	})
}

func writeDoctorConfig(t *testing.T, cfg *config.Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	return path
}

func validDoctorProfile() config.Profile {
	return config.Profile{
		Endpoint:       "https://pve.lan:8006/api2/json",
		TokenID:        "automation@pve!pvectl",
		TokenSecretEnv: "PVECTL_DOCTOR_TOKEN",
		Timeout:        "30s",
		DefaultOutput:  "table",
	}
}

func doctorFactory(backend Backend, err error) func(config.Profile, ClientOptions) (Backend, error) {
	return func(config.Profile, ClientOptions) (Backend, error) {
		return backend, err
	}
}

type doctorBackend struct {
	nodes    []output.NodeRow
	nodesErr error
}

func (b doctorBackend) Nodes(context.Context) ([]output.NodeRow, error) {
	return b.nodes, b.nodesErr
}

func (b doctorBackend) Node(context.Context, string) (output.NodeDetail, error) {
	return output.NodeDetail{}, ErrNotFound
}

func (b doctorBackend) VMs(context.Context, string) ([]output.GuestRow, error) {
	return nil, nil
}

func (b doctorBackend) VM(context.Context, string, int) (Guest, error) {
	return nil, ErrNotFound
}

func (b doctorBackend) LXCs(context.Context, string) ([]output.GuestRow, error) {
	return nil, nil
}

func (b doctorBackend) LXC(context.Context, string, int) (Guest, error) {
	return nil, ErrNotFound
}

func (b doctorBackend) Backups(context.Context, string, string) ([]output.BackupRow, error) {
	return nil, nil
}

func (b doctorBackend) BackupGuest(context.Context, string, BackupOptions) (Task, error) {
	return nil, nil
}

func (b doctorBackend) Storages(context.Context, string) ([]output.StorageRow, error) {
	return nil, nil
}

func (b doctorBackend) Storage(context.Context, string, string) (output.StorageRow, error) {
	return output.StorageRow{}, ErrNotFound
}

func (b doctorBackend) StorageContents(context.Context, string, string) ([]output.StorageContentRow, error) {
	return nil, nil
}

func (b doctorBackend) Tasks(context.Context, string, TaskListBackendOptions) ([]output.TaskRow, error) {
	return nil, nil
}

func (b doctorBackend) Task(context.Context, string, string) (output.TaskRow, error) {
	return output.TaskRow{}, ErrNotFound
}

func (b doctorBackend) TaskLog(context.Context, string, string, TaskLogPage) ([]output.TaskLogRow, error) {
	return nil, nil
}

func (b doctorBackend) TaskHandle(string) (Task, error) {
	return nil, ErrNotFound
}

func (b doctorBackend) Restore(context.Context, RestoreOptions) (Task, error) {
	return nil, ErrNotFound
}

func (b doctorBackend) AgentPing(context.Context, string, int) error {
	return ErrNotFound
}

func (b doctorBackend) AgentNetwork(context.Context, string, int) ([]output.AgentNetworkRow, error) {
	return nil, ErrNotFound
}

func (b doctorBackend) AgentExec(context.Context, string, int, AgentExecOptions) (output.AgentExecResult, error) {
	return output.AgentExecResult{}, ErrNotFound
}
