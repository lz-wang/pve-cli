package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
)

// The remote command runs the preflight plan first, transfers credentials
// only with --yes, and never echoes the target token.
func TestLXCMigrateRemoteCommandEndToEnd(t *testing.T) {
	backend := migrationPlanBackends()
	guest := &commandGuest{
		row:  backend.source.lxcGuests["n150"][101].row,
		task: &commandTask{upid: "UPID:n150:rm:101"},
	}
	backend.source.lxcGuests["n150"][101] = guest
	var stdout, stderr bytes.Buffer
	deps := migrationDeps(backend, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "migrate", "remote", "101",
		"--to", "pve8845", "--target-node", "pve8845", "--target-vmid", "201",
		"--storage-map", "local-lvm=local-lvm", "--bridge-map", "vmbr2=vmbr0",
		"--yes", "-o", "json",
	}, "test", deps)
	if err != nil {
		t.Fatalf("remote migrate: %v (stderr %s)", err, stderr.String())
	}
	if guest.remoteMigrateOptions == nil {
		t.Fatal("remote migrate not submitted")
	}
	options := guest.remoteMigrateOptions
	if options.TargetVMID != 201 || options.Online {
		t.Fatalf("options = %+v", options)
	}
	if !strings.Contains(options.TargetEndpointBundle, "host=pve8845.lan,port=8006") {
		t.Fatalf("bundle = %q", options.TargetEndpointBundle)
	}
	if !strings.Contains(options.TargetEndpointBundle, "apitoken=PVEAPIToken=root@pam!test=") {
		t.Fatalf("bundle missing token reference: %q", options.TargetEndpointBundle)
	}
	for _, stream := range []string{stdout.String(), stderr.String()} {
		if strings.Contains(stream, "apitoken=PVEAPIToken") {
			t.Fatalf("target credentials leaked into output: %s", stream)
		}
	}
	if !strings.Contains(stderr.String(), "receives target API credentials") {
		t.Fatalf("credential notice missing: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "experimental") {
		t.Fatalf("experimental notice missing: %s", stderr.String())
	}
}

func TestLXCMigrateRemoteRequiresYes(t *testing.T) {
	backend := migrationPlanBackends()
	guest := &commandGuest{row: backend.source.lxcGuests["n150"][101].row}
	backend.source.lxcGuests["n150"][101] = guest
	var stdout, stderr bytes.Buffer
	deps := migrationDeps(backend, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "migrate", "remote", "101",
		"--to", "pve8845", "--target-node", "pve8845",
	}, "test", deps)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v", err)
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if guest.remoteMigrateOptions != nil {
		t.Fatal("no submission without --yes")
	}
}

// P1-5: with -o json the submitted operation result (UPID and status) must
// be written to stdout so agents can track the task.
func TestLXCMigrateRemoteJSONOutputsOperationResult(t *testing.T) {
	backend := migrationPlanBackends()
	guest := &commandGuest{
		row:  backend.source.lxcGuests["n150"][101].row,
		task: &commandTask{upid: "UPID:n150:rm:101"},
	}
	backend.source.lxcGuests["n150"][101] = guest
	var stdout, stderr bytes.Buffer
	deps := migrationDeps(backend, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "migrate", "remote", "101",
		"--to", "pve8845", "--target-node", "pve8845",
		"--storage-map", "local-lvm=local-lvm", "--bridge-map", "vmbr2=vmbr0",
		"--yes", "-o", "json",
	}, "test", deps)
	if err != nil {
		t.Fatalf("remote migrate: %v (stderr %s)", err, stderr.String())
	}
	var result output.OperationResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("stdout must carry the operation result json: %v (%q)", err, stdout.String())
	}
	if result.Resource != "lxc" || result.Status != "submitted" || result.Task != "UPID:n150:rm:101" {
		t.Fatalf("result = %+v", result)
	}
}

// A submission failure must fail the command and write a non-succeeded
// result, never a silent success.
func TestLXCMigrateRemoteSubmissionFailureFailsCommand(t *testing.T) {
	backend := migrationPlanBackends()
	guest := &commandGuest{
		row:              backend.source.lxcGuests["n150"][101].row,
		task:             &commandTask{upid: "UPID:n150:rm:101"},
		remoteMigrateErr: errors.New("connection reset while submitting"),
	}
	backend.source.lxcGuests["n150"][101] = guest
	var stdout, stderr bytes.Buffer
	deps := migrationDeps(backend, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "migrate", "remote", "101",
		"--to", "pve8845", "--target-node", "pve8845",
		"--storage-map", "local-lvm=local-lvm", "--bridge-map", "vmbr2=vmbr0",
		"--yes", "-o", "json",
	}, "test", deps)
	if err == nil {
		t.Fatalf("submission failure must fail the command (stdout %s)", stdout.String())
	}
	var result output.OperationResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("stdout must carry the operation result json: %v (%q)", err, stdout.String())
	}
	if result.Status == "succeeded" {
		t.Fatalf("submission failure reported as succeeded: %+v", result)
	}
	if result.Status != "unknown" {
		t.Fatalf("status = %q, want unknown for an unconfirmed submission", result.Status)
	}
}

// P1-4: an HTTP 500 remote_migrate submission must agree across the two
// automation interfaces: the JSON result reads status unknown AND the
// process exit code is 5 (unknown final outcome, cannot safely retry) —
// never 3 (unavailable, transient), which invites an unsafe rerun. The
// classified API error (HTTP status, message) stays in the chain.
func TestLXCMigrateRemoteHTTP500JSONUnknownWithExitFive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/cluster/resources"):
			_, _ = w.Write([]byte(`{"data":[{"id":"lxc/101","vmid":101,"name":"app","node":"n150","type":"lxc","status":"stopped"}]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/lxc/101/status/current"):
			_, _ = w.Write([]byte(`{"data":{"vmid":101,"status":"stopped","name":"app"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/lxc/101/config"):
			_, _ = w.Write([]byte(`{"data":{"rootfs":"local-lvm:vm-101-disk-0,size=8G","net0":"name=eth0,bridge=vmbr2"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/nodes/n150/status"):
			_, _ = w.Write([]byte(`{"data":{"status":"online"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/lxc/101/remote_migrate"):
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"data":null,"message":"migration server error"}`))
		default:
			t.Logf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	backend := migrationPlanBackends()
	sourceBackend, err := pve.NewProxmoxBackend(config.Profile{Endpoint: server.URL, TokenID: "test@pve!cli"}, pve.ClientOptions{TokenSecret: "private-test-token"})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	deps := Dependencies{Stdout: &stdout, Stderr: &stderr}
	deps.BackendFactory = func(profile config.Profile, _ pve.ClientOptions) (pve.Backend, error) {
		if strings.Contains(profile.Endpoint, "pve8845") {
			return backend.target, nil
		}
		return sourceBackend, nil
	}

	err = RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "migrate", "remote", "101",
		"--to", "pve8845", "--target-node", "pve8845",
		"--storage-map", "local-lvm=local-lvm", "--bridge-map", "vmbr2=vmbr0",
		"--yes", "-o", "json",
	}, "test", deps)
	if err == nil {
		t.Fatalf("an HTTP 500 submission must fail the command (stdout %s)", stdout.String())
	}
	var result output.OperationResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("stdout must carry the operation result json: %v (%q)", err, stdout.String())
	}
	if result.Status != "unknown" {
		t.Fatalf("json status = %q, want unknown", result.Status)
	}
	if code := ExitCode(err); code != 5 {
		t.Fatalf("exit code = %d, want 5 for an unconfirmed outcome (err %v)", code, err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v, want the preserved HTTP status", err)
	}
}

// A failing preflight aborts before any mutation.
func TestLXCMigrateRemoteAbortsOnFailedPreflight(t *testing.T) {
	backend := migrationPlanBackends()
	backend.target.networkRows = map[string][]output.NetworkRow{} // bridge missing
	guest := &commandGuest{row: backend.source.lxcGuests["n150"][101].row}
	backend.source.lxcGuests["n150"][101] = guest
	var stdout, stderr bytes.Buffer
	deps := migrationDeps(backend, &stdout, &stderr)

	err := RunWithDependencies([]string{
		"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150",
		"lxc", "migrate", "remote", "101",
		"--to", "pve8845", "--target-node", "pve8845", "--yes",
	}, "test", deps)
	if err == nil || !strings.Contains(err.Error(), "preflight") {
		t.Fatalf("err = %v", err)
	}
	if guest.remoteMigrateOptions != nil {
		t.Fatal("mutation must not run after failed preflight")
	}
}

// P0-2: the target-endpoint bundle must use PVE's official comma-separated
// attribute format with separate host/port fields. The CLI map syntax stays
// source=target; the submitted form is source:target.
func TestBuildTargetEndpointBundleOfficialFormat(t *testing.T) {
	t.Setenv("PVE_BUNDLE_TOKEN", "bundle-secret-0123456789")
	cases := []struct {
		name        string
		endpoint    string
		fingerprint string
		expected    string
	}{
		{
			"ipv4 with default port",
			"https://10.0.0.2:8006/api2/json",
			"",
			"apitoken=PVEAPIToken=root@pam!tgt=bundle-secret-0123456789,host=10.0.0.2,port=8006",
		},
		{
			"hostname without port defers to the PVE default",
			"https://pve.lan/api2/json",
			"",
			"apitoken=PVEAPIToken=root@pam!tgt=bundle-secret-0123456789,host=pve.lan",
		},
		{
			"non-default port is explicit",
			"https://pve.lan:8007/api2/json",
			"",
			"apitoken=PVEAPIToken=root@pam!tgt=bundle-secret-0123456789,host=pve.lan,port=8007",
		},
		{
			"ipv6 host loses the brackets",
			"https://[2001:db8::10]:8006/api2/json",
			"",
			"apitoken=PVEAPIToken=root@pam!tgt=bundle-secret-0123456789,host=2001:db8::10,port=8006",
		},
		{
			"fingerprint is a separate attribute",
			"https://pve.lan:8006/api2/json",
			"AA:BB:CC",
			"apitoken=PVEAPIToken=root@pam!tgt=bundle-secret-0123456789,host=pve.lan,port=8006,fingerprint=AA:BB:CC",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bundle, err := buildTargetEndpointBundle(config.Profile{
				Endpoint:       tc.endpoint,
				TokenID:        "root@pam!tgt",
				TokenSecretEnv: "PVE_BUNDLE_TOKEN",
			}, tc.fingerprint)
			if err != nil {
				t.Fatalf("bundle: %v", err)
			}
			if bundle != tc.expected {
				t.Fatalf("bundle = %q, want %q", bundle, tc.expected)
			}
		})
	}
}

// P0-4: when the source PVE rejects the migration and echoes the submitted
// target-endpoint (which embeds the target token secret), the secret must
// never reach stdout, stderr or the returned error. This drives the real CLI
// chain: both profiles are real HTTP backends against the same test server.
func TestMigrateRemoteTargetSecretRedactedThroughCLI(t *testing.T) {
	const targetSecret = "tgt-secret-0123456789abcdef"
	var remoteMigrateBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/cluster/resources"):
			_, _ = w.Write([]byte(`{"data":[{"type":"lxc","vmid":101,"node":"n150","name":"app","status":"stopped"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/nodes":
			_, _ = w.Write([]byte(`{"data":[{"node":"pve8845","status":"online"},{"node":"n150","status":"online"}]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/nodes/n150/status"),
			r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/nodes/pve8845/status"):
			_, _ = w.Write([]byte(`{"data":{"status":"online"}}`))
		case r.Method == http.MethodGet && (strings.HasSuffix(r.URL.Path, "/qemu") || strings.HasSuffix(r.URL.Path, "/lxc")):
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/nodes/n150/lxc/101/status/current"):
			_, _ = w.Write([]byte(`{"data":{"vmid":101,"status":"stopped","name":"app"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/nodes/n150/lxc/101/config"):
			_, _ = w.Write([]byte(`{"data":{"rootfs":"local-lvm:vm-101-disk-0,size=8G","net0":"name=eth0,bridge=vmbr2"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/nodes/pve8845/storage"):
			_, _ = w.Write([]byte(`{"data":[{"storage":"local-lvm","type":"lvmthin","active":1,"enabled":1,"content":"rootdir,images","avail":17179869184}]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/nodes/pve8845/network"):
			_, _ = w.Write([]byte(`{"data":[{"iface":"vmbr0","type":"bridge","active":1,"autostart":1}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/lxc/101/remote_migrate"):
			body, _ := io.ReadAll(r.Body)
			remoteMigrateBody = body
			w.WriteHeader(http.StatusBadRequest)
			// The server echoes the submitted bundle back, secret included.
			_, _ = fmt.Fprintf(w, `{"data":null,"message":"remote endpoint rejected","errors":{"target-endpoint":%q}}`, string(body))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.Empty()
	mustSet := func(name string, secretEnv string) {
		t.Helper()
		if err := cfg.SetProfile(name, config.Profile{
			Endpoint:       server.URL,
			TokenID:        "root@pam!test",
			TokenSecretEnv: secretEnv,
		}); err != nil {
			t.Fatalf("set profile %s: %v", name, err)
		}
	}
	t.Setenv("PVE_SOURCE_TOKEN", "source-secret-0123456789abcdef")
	t.Setenv("PVE_TARGET_TOKEN", targetSecret)
	mustSet("n150", "PVE_SOURCE_TOKEN")
	mustSet("pve8845", "PVE_TARGET_TOKEN")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	deps := Dependencies{Stdout: &stdout, Stderr: &stderr} // real HTTP backends
	err := RunWithDependencies([]string{
		"pve", "--config", cfgPath, "--profile", "n150",
		"lxc", "migrate", "remote", "101",
		"--to", "pve8845", "--target-node", "pve8845", "--target-vmid", "201",
		"--storage-map", "local-lvm=local-lvm", "--bridge-map", "vmbr2=vmbr0",
		"--yes", "-o", "json",
	}, "test", deps)
	if err == nil {
		t.Fatal("remote migrate must fail on HTTP 400")
	}
	if len(remoteMigrateBody) == 0 || !bytes.Contains(remoteMigrateBody, []byte(targetSecret)) {
		t.Fatal("precondition: the submission must carry the target secret")
	}
	for stream, name := range map[string]string{stdout.String(): "stdout", stderr.String(): "stderr", err.Error(): "error"} {
		if strings.Contains(stream, targetSecret) {
			t.Fatalf("target secret leaked into %s", name)
		}
	}
}
