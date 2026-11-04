package pve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/output"
)

type remoteMigrateGuest struct {
	*fakeGuest
	options RemoteMigrateOptions
	called  bool
	task    Task
	err     error
}

func (g *remoteMigrateGuest) RemoteMigrate(_ context.Context, options RemoteMigrateOptions) (Task, error) {
	g.called = true
	g.options = options
	return g.task, g.err
}

type remoteMigrateBackend struct {
	*fakeBackend
	guest Guest
	// config backs the native-config read the same-name map derivation
	// needs; nil means the capability is missing (fail-closed).
	config map[string]any
}

func (b *remoteMigrateBackend) LXC(ctx context.Context, node string, vmid int) (Guest, error) {
	if int(b.guest.Row().VMID) == vmid {
		return b.guest, nil
	}
	return b.fakeBackend.LXC(ctx, node, vmid)
}

func (b *remoteMigrateBackend) GuestConfig(_ context.Context, _, _ string, _ int) (map[string]any, error) {
	if b.config == nil {
		return nil, fmt.Errorf("%w: native config unavailable", ErrUnavailable)
	}
	return b.config, nil
}

func remoteMigrateSetup(t *testing.T, guest *remoteMigrateGuest, wait bool) (*GuestService, *bytes.Buffer) {
	t.Helper()
	backend := &remoteMigrateBackend{
		fakeBackend: &fakeBackend{
			nodes: []output.NodeRow{{Name: "n150", Status: "online"}},
			lxcs:  map[string]map[int]*fakeGuest{"n150": {101: guest.fakeGuest}},
		},
		guest: guest,
		config: map[string]any{
			"rootfs": "local-lvm:vm-101-disk-0,size=8G",
			"net0":   "name=eth0,bridge=vmbr2",
		},
	}
	var out bytes.Buffer
	runner := TaskRunner{Wait: wait, WaitTimeout: time.Second, Format: "json", ResultWriter: &out}
	return NewLXCService(backend, runner, nil, false), &out
}

func newRemoteMigrateGuest() *remoteMigrateGuest {
	return &remoteMigrateGuest{fakeGuest: &fakeGuest{row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "n150", Status: "stopped"}}}
}

// The defaults implement the safety constraints: offline, keep the source,
// do not autostart the target.
func TestRemoteMigrateSubmitsOfflineDefaults(t *testing.T) {
	guest := newRemoteMigrateGuest()
	guest.task = &fakeTask{upid: "UPID:n150:rm:101:"}
	svc, out := remoteMigrateSetup(t, guest, false)

	_, err := svc.RemoteMigrate(context.Background(), 101, "n150", RemoteMigrateOptions{
		TargetEndpointBundle: "apitoken=PVEAPIToken=u@pam!t=secret,host=t.lan",
		TargetStorageMap:     map[string]string{"local-lvm": "local-lvm"},
		TargetBridgeMap:      map[string]string{"vmbr2": "vmbr0"},
		TargetVMID:           201,
	})
	if err != nil {
		t.Fatalf("remote migrate: %v", err)
	}
	if !guest.called {
		t.Fatal("RemoteMigrate not called")
	}
	if guest.options.Online {
		t.Fatalf("unsafe default: online migration requested: %+v", guest.options)
	}
	if guest.options.TargetVMID != 201 || guest.options.TargetStorageMap["local-lvm"] != "local-lvm" {
		t.Fatalf("options = %+v", guest.options)
	}
	rendered := out.String()
	if strings.Contains(rendered, "apitoken=") || strings.Contains(rendered, "secret") {
		t.Fatalf("target credentials leaked into result: %s", rendered)
	}
	var result output.OperationResult
	if err := json.Unmarshal([]byte(rendered), &result); err != nil {
		t.Fatalf("json: %v", err)
	}
	if result.Resource != "lxc" || result.Node != "n150" || result.Task == "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestRemoteMigrateValidations(t *testing.T) {
	guest := newRemoteMigrateGuest()
	svc, _ := remoteMigrateSetup(t, guest, false)

	if _, err := svc.RemoteMigrate(context.Background(), 0, "n150", RemoteMigrateOptions{TargetEndpointBundle: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if _, err := svc.RemoteMigrate(context.Background(), 101, "n150", RemoteMigrateOptions{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want missing endpoint bundle rejected", err)
	}
	if guest.called {
		t.Fatal("runner called for invalid options")
	}
}

// The HTTP contract: one POST to remote_migrate with the mapped parameters;
// the delete flag is never sent.
func TestRemoteMigrateHTTPContract(t *testing.T) {
	var body map[string]any
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/lxc/101/status/current"):
			_, _ = w.Write([]byte(`{"data":{"vmid":101,"status":"stopped","name":"app"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/lxc/101/config"):
			_, _ = w.Write([]byte(`{"data":{"rootfs":"local-lvm:vm-101-disk-0,size=8G","net0":"name=eth0,bridge=vmbr2"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/lxc/101/remote_migrate"):
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(`{"data":"UPID:n150:0001:0000:6839F4A1:vzsrmigrate:101:root@pam:"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/nodes/n150/status"):
			_, _ = w.Write([]byte(`{"data":{"status":"online"}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	backend, err := NewProxmoxBackend(config.Profile{Endpoint: server.URL, TokenID: "test@pve!cli"}, ClientOptions{TokenSecret: "private-test-token"})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewLXCService(backend, TaskRunner{}, nil, false)

	if _, err := svc.RemoteMigrate(context.Background(), 101, "n150", RemoteMigrateOptions{
		TargetEndpointBundle: "apitoken=PVEAPIToken=root@pam!tgt=0123456789abcdef,host=10.0.0.2,port=8006,fingerprint=AA:BB:CC",
		TargetStorageMap:     map[string]string{"local-lvm": "local-lvm"},
		TargetBridgeMap:      map[string]string{"vmbr2": "vmbr0"},
		TargetVMID:           201,
	}); err != nil {
		t.Fatalf("remote migrate: %v", err)
	}
	if body["target-endpoint"] != "apitoken=PVEAPIToken=root@pam!tgt=0123456789abcdef,host=10.0.0.2,port=8006,fingerprint=AA:BB:CC" {
		t.Fatalf("target-endpoint = %v", body["target-endpoint"])
	}
	if body["target-storage"] != "local-lvm:local-lvm" || body["target-bridge"] != "vmbr2:vmbr0" {
		t.Fatalf("maps = %v %v", body["target-storage"], body["target-bridge"])
	}
	if body["target-vmid"] != float64(201) {
		t.Fatalf("target-vmid = %v", body["target-vmid"])
	}
	if _, exists := body["delete"]; exists {
		t.Fatalf("delete must never be sent: %v", body)
	}
	if _, exists := body["online"]; exists {
		t.Fatalf("online must default to absent: %v", body)
	}
	if len(paths) == 0 {
		t.Fatal("no requests made")
	}
}

// P0-1/P0-3 regression: a failed or unconfirmed submission must never
// surface as a succeeded result. Only a provable rejection — the 4xx family,
// or a server error whose body names a missing resource (a lookup failure
// cannot have started anything) — marks the outcome failed. Server failures
// (500/502/503/504) and transport errors leave the outcome unproven
// (unknown): HTTP 500 does not prove the migration did not start. A UPID
// returned alongside an error is preserved so the task can be queried. A
// nil task or an empty UPID can never mean success either.
func TestRemoteMigrateSubmissionFailureNeverReportsSuccess(t *testing.T) {
	cases := []struct {
		name         string
		task         Task
		err          error
		wantStatus   string
		wantKind     error
		wantTaskKeep string
	}{
		{"http 400", nil, &APIError{HTTPStatus: 400, Category: CategoryInvalid, kind: ErrInvalid}, "failed", nil, ""},
		{"http 408", nil, &APIError{HTTPStatus: 408, Category: CategoryUnavailable, kind: ErrUnavailable}, "unknown", nil, ""},
		{"http 409", nil, &APIError{HTTPStatus: 409, Category: CategoryConflict, kind: ErrConflict}, "failed", nil, ""},
		{"http 412", nil, &APIError{HTTPStatus: 412, Category: CategoryConflict, kind: ErrConflict}, "failed", nil, ""},
		{"http 500", nil, &APIError{HTTPStatus: 500, Category: CategoryUnavailable, kind: ErrUnavailable}, "unknown", nil, ""},
		{"http 502", nil, &APIError{HTTPStatus: 502, Category: CategoryUnavailable, kind: ErrUnavailable}, "unknown", nil, ""},
		{"http 503", nil, &APIError{HTTPStatus: 503, Category: CategoryUnavailable, kind: ErrUnavailable}, "unknown", nil, ""},
		{"http 504", nil, &APIError{HTTPStatus: 504, Category: CategoryUnavailable, kind: ErrUnavailable}, "unknown", nil, ""},
		// P1-4: a 500 body naming a missing resource is an ErrNotFound
		// classification, never a proven rejection — the lookup can happen
		// in a phase after the migration already started.
		{"http 500 lookup failure", nil, &APIError{HTTPStatus: 500, Category: CategoryNotFound, Message: "no such guest", kind: ErrNotFound}, "unknown", nil, ""},
		{"network timeout", nil, context.DeadlineExceeded, "unknown", ErrUnavailable, ""},
		{"nil task without error", nil, nil, "unknown", ErrTaskTimeout, ""},
		{"empty upid", &fakeTask{}, nil, "unknown", ErrTaskTimeout, ""},
		{"upid preserved on uncertain error", &fakeTask{upid: "UPID:n150:rm:101:"}, context.DeadlineExceeded, "unknown", ErrUnavailable, "UPID:n150:rm:101:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			guest := newRemoteMigrateGuest()
			guest.task = tc.task
			guest.err = tc.err
			svc, out := remoteMigrateSetup(t, guest, false)

			operation, err := svc.RemoteMigrate(context.Background(), 101, "n150", RemoteMigrateOptions{
				TargetEndpointBundle: "apitoken=PVEAPIToken=u@pam!t=secret host=t.lan",
			})
			if err == nil {
				t.Fatalf("expected error, got result %+v", operation)
			}
			if tc.wantKind != nil && !errors.Is(err, tc.wantKind) {
				t.Fatalf("err = %v, want kind %v", err, tc.wantKind)
			}
			if tc.wantKind == nil {
				var apiErr *APIError
				if !errors.As(err, &apiErr) {
					t.Fatalf("err = %v, want preserved *APIError", err)
				}
			}
			if operation.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", operation.Status, tc.wantStatus)
			}
			rendered := out.String()
			if rendered == "" {
				t.Fatal("result must still be written for a failed submission")
			}
			if strings.Contains(rendered, "succeeded") {
				t.Fatalf("submission failure reported as succeeded: %s", rendered)
			}
			var result output.OperationResult
			if err := json.Unmarshal([]byte(rendered), &result); err != nil {
				t.Fatalf("json: %v", err)
			}
			if result.Status != tc.wantStatus {
				t.Fatalf("written status = %q, want %q", result.Status, tc.wantStatus)
			}
			if tc.wantTaskKeep != "" && result.Task != tc.wantTaskKeep {
				t.Fatalf("task = %q, want the preserved UPID %q", result.Task, tc.wantTaskKeep)
			}
		})
	}
}

// P0-1 HTTP regression through the real client: an HTTP 400 from
// remote_migrate must fail the command and write a failed result.
func TestRemoteMigrateHTTP400FailsTheCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/lxc/101/status/current"):
			_, _ = w.Write([]byte(`{"data":{"vmid":101,"status":"stopped","name":"app"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/lxc/101/config"):
			_, _ = w.Write([]byte(`{"data":{"rootfs":"local-lvm:vm-101-disk-0,size=8G","net0":"name=eth0,bridge=vmbr2"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/nodes/n150/status"):
			_, _ = w.Write([]byte(`{"data":{"status":"online"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/lxc/101/remote_migrate"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"data":null,"errors":{"target-vmid":"VM 201 is already in use"}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	backend, err := NewProxmoxBackend(config.Profile{Endpoint: server.URL, TokenID: "test@pve!cli"}, ClientOptions{TokenSecret: "private-test-token"})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	svc := NewLXCService(backend, TaskRunner{Format: "json", ResultWriter: &out}, nil, false)

	operation, err := svc.RemoteMigrate(context.Background(), 101, "n150", RemoteMigrateOptions{
		TargetEndpointBundle: "apitoken=PVEAPIToken=root@pam!tgt=secret host=10.0.0.2:8006",
		TargetVMID:           201,
	})
	if err == nil {
		t.Fatalf("expected error, got result %+v", operation)
	}
	var result output.OperationResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("json %q: %v", out.String(), err)
	}
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
}

func TestFormatMigrationMaps(t *testing.T) {
	cases := []struct {
		input    map[string]string
		expected string
	}{
		{nil, ""},
		{map[string]string{"a": "b"}, "a:b"},
		{map[string]string{"a": "b", "c": "d"}, "a:b,c:d"},
	}
	for _, tc := range cases {
		if got := formatMigrationMaps(tc.input); got != tc.expected {
			t.Fatalf("format(%v) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// The target API credentials must be redacted by the source transport even
// though they belong to a different profile.
func TestRemoteMigrateRedactsTargetSecret(t *testing.T) {
	const targetSecret = "tgt-secret-0123456789abcdef"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, `{"data":null,"message":"remote endpoint rejected apitoken with secret %s"}`, targetSecret)
	}))
	defer server.Close()

	backend, err := NewProxmoxBackend(config.Profile{Endpoint: server.URL, TokenID: "test@pve!cli"}, ClientOptions{
		TokenSecret:  "private-test-token",
		ExtraSecrets: []string{targetSecret},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewLXCService(backend, TaskRunner{}, nil, false)

	_, err = svc.RemoteMigrate(context.Background(), 101, "n150", RemoteMigrateOptions{
		TargetEndpointBundle: "apitoken=PVEAPIToken=root@pam!tgt=" + targetSecret + " host=x",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), targetSecret) {
		t.Fatalf("target secret leaked: %v", err)
	}
}

// P0-3 HTTP regression through the real client: an HTTP 500 from
// remote_migrate leaves the outcome unproven (unknown) — the server may
// have started the migration before failing — while an HTTP 400 stays a
// provable rejection (failed). A 500 body naming a missing resource does
// not change this: the lookup failure can happen in a phase after the
// migration already started, so every 5xx stays unknown.
func TestRemoteMigrateHTTP500StaysUnknown(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		wantStatus string
	}{
		{"Internal Server Error", http.StatusInternalServerError, `{"data":null,"message":"migration server error"}`, "unknown"},
		{"500 body names missing resource", http.StatusInternalServerError, `{"data":null,"message":"guest '201' not found"}`, "unknown"},
		{"500 body says does not exist", http.StatusInternalServerError, `{"data":null,"message":"storage 'local' does not exist"}`, "unknown"},
		{"Bad Gateway", http.StatusBadGateway, `{"data":null,"message":"migration server error"}`, "unknown"},
		{"Service Unavailable", http.StatusServiceUnavailable, `{"data":null,"message":"migration server error"}`, "unknown"},
		{"Gateway Timeout", http.StatusGatewayTimeout, `{"data":null,"message":"migration server error"}`, "unknown"},
		{"Bad Request", http.StatusBadRequest, `{"data":null,"message":"migration server error"}`, "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/lxc/101/status/current"):
					_, _ = w.Write([]byte(`{"data":{"vmid":101,"status":"stopped","name":"app"}}`))
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/lxc/101/config"):
					_, _ = w.Write([]byte(`{"data":{"rootfs":"local-lvm:vm-101-disk-0,size=8G","net0":"name=eth0,bridge=vmbr2"}}`))
				case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/nodes/n150/status"):
					_, _ = w.Write([]byte(`{"data":{"status":"online"}}`))
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/lxc/101/remote_migrate"):
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			backend, err := NewProxmoxBackend(config.Profile{Endpoint: server.URL, TokenID: "test@pve!cli"}, ClientOptions{TokenSecret: "private-test-token"})
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			svc := NewLXCService(backend, TaskRunner{Format: "json", ResultWriter: &out}, nil, false)

			operation, err := svc.RemoteMigrate(context.Background(), 101, "n150", RemoteMigrateOptions{
				TargetEndpointBundle: "apitoken=PVEAPIToken=root@pam!tgt=secret host=10.0.0.2:8006",
				TargetVMID:           201,
			})
			if err == nil {
				t.Fatalf("expected error, got result %+v", operation)
			}
			var result output.OperationResult
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatalf("json %q: %v", out.String(), err)
			}
			if result.Status != tc.wantStatus {
				t.Fatalf("HTTP %d: status = %q, want %q", tc.status, result.Status, tc.wantStatus)
			}
		})
	}
}

// P2: without explicit storage/bridge maps, the service derives explicit
// same-name mappings from the guest configuration — PVE's remote_migrate
// API expects the mapping parameters, and omitting them risks a server-side
// 400 before the migration can even start.
func TestRemoteMigrateDerivesSameNameMaps(t *testing.T) {
	guest := newRemoteMigrateGuest()
	guest.task = &fakeTask{upid: "UPID:n150:rm:101:"}
	backend := &remoteMigrateBackend{
		fakeBackend: &fakeBackend{
			nodes: []output.NodeRow{{Name: "n150", Status: "online"}},
			lxcs:  map[string]map[int]*fakeGuest{"n150": {101: guest.fakeGuest}},
		},
		guest: guest,
		config: map[string]any{
			"rootfs":  "local-lvm:vm-101-disk-0,size=8G",
			"mp0":     "local-lvm:vm-101-disk-1,size=4G,mp=/data",
			"net0":    "name=eth0,bridge=vmbr2,ip=dhcp",
			"unused0": "local-lvm:vm-101-disk-2",
		},
	}
	var out bytes.Buffer
	runner := TaskRunner{Wait: false, WaitTimeout: time.Second, Format: "json", ResultWriter: &out}
	svc := NewLXCService(backend, runner, nil, false)

	_, err := svc.RemoteMigrate(context.Background(), 101, "n150", RemoteMigrateOptions{
		TargetEndpointBundle: "apitoken=PVEAPIToken=u@pam!t=secret,host=t.lan",
		TargetVMID:           201,
	})
	if err != nil {
		t.Fatalf("remote migrate: %v", err)
	}
	if guest.options.TargetStorageMap["local-lvm"] != "local-lvm" {
		t.Fatalf("storage map = %v, want explicit same-name mapping", guest.options.TargetStorageMap)
	}
	if guest.options.TargetBridgeMap["vmbr2"] != "vmbr2" {
		t.Fatalf("bridge map = %v, want explicit same-name mapping", guest.options.TargetBridgeMap)
	}
	if len(guest.options.TargetStorageMap) != 1 {
		t.Fatalf("storage map = %v, exactly the referenced storage must be mapped", guest.options.TargetStorageMap)
	}
}
