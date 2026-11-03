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
}

func (b *remoteMigrateBackend) LXC(ctx context.Context, node string, vmid int) (Guest, error) {
	if int(b.guest.Row().VMID) == vmid {
		return b.guest, nil
	}
	return b.fakeBackend.LXC(ctx, node, vmid)
}

func remoteMigrateSetup(t *testing.T, guest *remoteMigrateGuest, wait bool) (*GuestService, *bytes.Buffer) {
	t.Helper()
	backend := &remoteMigrateBackend{
		fakeBackend: &fakeBackend{
			nodes: []output.NodeRow{{Name: "n150", Status: "online"}},
			lxcs:  map[string]map[int]*fakeGuest{"n150": {101: guest.fakeGuest}},
		},
		guest: guest,
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
		TargetEndpointBundle: "apitoken=PVEAPIToken=u@pam!t=secret host=t.lan",
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
		TargetEndpointBundle: "apitoken=PVEAPIToken=root@pam!tgt=0123456789abcdef host=10.0.0.2:8006 fingerprint=AA:BB:CC",
		TargetStorageMap:     map[string]string{"local-lvm": "local-lvm"},
		TargetBridgeMap:      map[string]string{"vmbr2": "vmbr0"},
		TargetVMID:           201,
	}); err != nil {
		t.Fatalf("remote migrate: %v", err)
	}
	if body["target-endpoint"] != "apitoken=PVEAPIToken=root@pam!tgt=0123456789abcdef host=10.0.0.2:8006 fingerprint=AA:BB:CC" {
		t.Fatalf("target-endpoint = %v", body["target-endpoint"])
	}
	if body["target-storage"] != "local-lvm=local-lvm" || body["target-bridge"] != "vmbr2=vmbr0" {
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

func TestFormatMigrationMaps(t *testing.T) {
	cases := []struct {
		input    map[string]string
		expected string
	}{
		{nil, ""},
		{map[string]string{"a": "b"}, "a=b"},
		{map[string]string{"a": "b", "c": "d"}, "a=b,c=d"},
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
