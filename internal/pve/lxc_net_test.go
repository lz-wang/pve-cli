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

type netTestBackend struct {
	*verifyingBackend
}

// sequencingConfigBackend returns the pre-write config on the first read and
// the post-write state afterwards, mirroring a server that applied the PUT.
type sequencingConfigBackend struct {
	*netTestBackend
	calls   int
	updated map[string]any
}

func (b *sequencingConfigBackend) GuestConfig(ctx context.Context, kind, node string, id int) (map[string]any, error) {
	b.calls++
	if b.calls >= 2 {
		return b.updated, nil
	}
	return b.netTestBackend.GuestConfig(ctx, kind, node, id)
}

// newNetService wires an LXC service over fakes. When postConfig is set, the
// first config read returns preConfig (pre-write) and later reads return
// postConfig, mirroring a server that applied the PUT.
func newNetService(t *testing.T, preConfig, postConfig map[string]any, bridges []output.NetworkRow, wait bool) (*GuestService, *netTestBackend, *bytes.Buffer) {
	t.Helper()
	guest := &fakeGuest{row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve1"}}
	base := &verifyingBackend{fakeBackend: &fakeBackend{
		nodes:       []output.NodeRow{{Name: "pve1"}},
		lxcs:        map[string]map[int]*fakeGuest{"pve1": {101: guest}},
		networkRows: map[string][]output.NetworkRow{"pve1": bridges},
	}, config: preConfig}
	backend := &netTestBackend{verifyingBackend: base}
	serviceBackend := GuestBackend(backend)
	if postConfig != nil {
		serviceBackend = &sequencingConfigBackend{netTestBackend: backend, updated: postConfig}
	}
	var out bytes.Buffer
	runner := TaskRunner{Wait: wait, WaitTimeout: time.Second, Format: "json", ResultWriter: &out}
	return NewLXCService(serviceBackend, runner, nil, false), backend, &out
}

// Updating one attribute preserves every other attribute of the device and
// submits the complete option string.
func TestLXCNetSetPreservesUnspecifiedAttributes(t *testing.T) {
	preConfig := map[string]any{
		"net0":   "name=eth0,bridge=vmbr2,hwaddr=BC:24:11:2F:AB:12,ip=192.168.2.10/24,gw=192.168.2.1,firewall=1",
		"digest": "abc123digest",
	}
	postConfig := map[string]any{
		"net0":   "name=eth0,bridge=vmbr0,hwaddr=BC:24:11:2F:AB:12,ip=192.168.2.10/24,gw=192.168.2.1,firewall=1",
		"digest": "newdigest",
	}
	service, backend, _ := newNetService(t, preConfig, postConfig,
		[]output.NetworkRow{{Node: "pve1", Name: "vmbr0", Type: "bridge"}}, false)

	err := service.UpdateLXCNet(context.Background(), 101, LXCNetUpdateOptions{
		Node:   "pve1",
		Device: "net0",
		Bridge: "vmbr0",
	})
	if err != nil {
		t.Fatalf("net set: %v", err)
	}
	values := backend.lxcs["pve1"][101].configValues
	submitted := values["net0"]
	if submitted != "name=eth0,bridge=vmbr0,hwaddr=BC:24:11:2F:AB:12,ip=192.168.2.10/24,gw=192.168.2.1,firewall=1" {
		t.Fatalf("submitted net0 = %q", submitted)
	}
	if values["digest"] != "abc123digest" {
		t.Fatalf("digest not submitted for optimistic concurrency: %+v", values)
	}
}

// The verification read compares the merged device value, not the partial
// patch; appended server attributes (hwaddr) are tolerated.
func TestLXCNetSetVerifiesMergedResult(t *testing.T) {
	service, _, out := newNetService(t,
		map[string]any{"net0": "name=eth0,bridge=vmbr2", "digest": "d1"},
		map[string]any{"net0": "name=eth0,bridge=vmbr0,hwaddr=BC:24:11:2F:AB:12"},
		[]output.NetworkRow{{Node: "pve1", Name: "vmbr0", Type: "bridge"}}, false)

	if err := service.UpdateLXCNet(context.Background(), 101, LXCNetUpdateOptions{Node: "pve1", Device: "net0", Bridge: "vmbr0"}); err != nil {
		t.Fatalf("net set: %v", err)
	}
	var result output.OperationResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Verified == nil || !*result.Verified {
		t.Fatalf("verified = %v, want true (%s)", result.Verified, out.String())
	}
	if result.Status != "succeeded" {
		t.Fatalf("status = %q", result.Status)
	}
}

func TestLXCNetSetValidations(t *testing.T) {
	cases := []struct {
		name    string
		options LXCNetUpdateOptions
		want    error
	}{
		{"device required", LXCNetUpdateOptions{Node: "pve1", Bridge: "vmbr0"}, ErrInvalid},
		{"device pattern", LXCNetUpdateOptions{Node: "pve1", Device: "eth0", Bridge: "vmbr0"}, ErrInvalid},
		{"no attributes", LXCNetUpdateOptions{Node: "pve1", Device: "net0"}, ErrInvalid},
		{"missing device in config", LXCNetUpdateOptions{Node: "pve1", Device: "net9", Bridge: "vmbr0"}, ErrNotFound},
		{"unknown bridge", LXCNetUpdateOptions{Node: "pve1", Device: "net0", Bridge: "vmbrX"}, ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configMap := map[string]any{"net0": "name=eth0,bridge=vmbr2", "digest": "d1"}
			service, _, _ := newNetService(t, configMap, nil,
				[]output.NetworkRow{{Node: "pve1", Name: "vmbr0", Type: "bridge"}}, false)
			err := service.UpdateLXCNet(context.Background(), 101, tc.options)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// New attributes append at the end of the option string without touching
// existing order.
func TestMergeNetAttributes(t *testing.T) {
	cases := []struct {
		name     string
		current  string
		updates  map[string]string
		expected string
	}{
		{
			name:     "replace in place",
			current:  "name=eth0,bridge=vmbr2,tag=5",
			updates:  map[string]string{"bridge": "vmbr0"},
			expected: "name=eth0,bridge=vmbr0,tag=5",
		},
		{
			name:     "append new",
			current:  "name=eth0,bridge=vmbr0",
			updates:  map[string]string{"tag": "10"},
			expected: "name=eth0,bridge=vmbr0,tag=10",
		},
		{
			name:     "replace dhcp ip",
			current:  "name=eth0,ip=dhcp,bridge=vmbr0",
			updates:  map[string]string{"ip": "192.168.1.5/24", "gw": "192.168.1.1"},
			expected: "name=eth0,ip=192.168.1.5/24,bridge=vmbr0,gw=192.168.1.1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mergeNetAttributes(tc.current, tc.updates); got != tc.expected {
				t.Fatalf("merge = %q, want %q", got, tc.expected)
			}
		})
	}
}

// The full HTTP contract: GET config (with digest), GET networks, PUT
// carries the complete merged net0 value plus digest.
func TestLXCNetSetHTTPContract(t *testing.T) {
	var putBody map[string]any
	applied := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/nodes/pve1/lxc/101/status/current"):
			_, _ = w.Write([]byte(`{"data":{"vmid":101,"status":"stopped","name":"ct101"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/lxc/101/config"):
			if applied {
				_, _ = w.Write([]byte(`{"data":{"net0":"name=eth0,bridge=vmbr0,hwaddr=BC:24:11:2F:AB:12","digest":"newdigest"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"net0":"name=eth0,bridge=vmbr2,hwaddr=BC:24:11:2F:AB:12","digest":"deadbeef"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/network"):
			_, _ = w.Write([]byte(`{"data":[{"iface":"vmbr0","type":"bridge","active":1,"method":"static"}]}`))
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/lxc/101/config"):
			if err := json.NewDecoder(r.Body).Decode(&putBody); err != nil {
				t.Error(err)
			}
			applied = true
			_, _ = w.Write([]byte(`{"data":null}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/nodes/pve1/status"):
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
	var out bytes.Buffer
	service := NewLXCService(backend, TaskRunner{Format: "json", ResultWriter: &out}, nil, false)

	if err := service.UpdateLXCNet(context.Background(), 101, LXCNetUpdateOptions{Node: "pve1", Device: "net0", Bridge: "vmbr0"}); err != nil {
		t.Fatalf("net set: %v", err)
	}
	if putBody["net0"] != "name=eth0,bridge=vmbr0,hwaddr=BC:24:11:2F:AB:12" {
		t.Fatalf("put net0 = %v", putBody["net0"])
	}
	if putBody["digest"] != "deadbeef" {
		t.Fatalf("put digest = %v", putBody["digest"])
	}
	if _, exists := putBody["node"]; exists {
		t.Fatalf("unexpected keys: %v", putBody)
	}
}

// A stale digest must surface as a conflict instead of a silent overwrite.
func TestLXCNetSetDigestConflictIsErrConflict(t *testing.T) {
	if kind := kindForHTTPStatus(http.StatusPreconditionFailed); !errors.Is(kind, ErrConflict) {
		t.Fatalf("412 kind = %v, want conflict", kind)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPut, "https://pve.invalid/api2/json/nodes/pve1/lxc/101/config", nil)
	transport := statusTransport{base: fixedTransport{resp: &http.Response{
		StatusCode: http.StatusPreconditionFailed,
		Status:     fmt.Sprintf("%d %s", http.StatusPreconditionFailed, http.StatusText(http.StatusPreconditionFailed)),
		Body:       newBytesBody(`{"data":null,"message":"configuration modified by someone else"}`),
		Header:     http.Header{},
		Request:    req,
	}}, redact: newErrorSanitizer("private-test-token")}
	_, err := transport.RoundTrip(req)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want conflict", err)
	}
}

// P2: a name match is not enough — only an actual bridge may receive a
// net device; an ordinary interface with the same name must be refused.
func TestLXCNetSetRejectsNonBridgeDevice(t *testing.T) {
	service, _, _ := newNetService(t,
		map[string]any{"net0": "name=eth0,bridge=vmbr2"},
		nil,
		[]output.NetworkRow{{Node: "pve1", Name: "vmbr0", Type: "ether"}}, false)

	err := service.UpdateLXCNet(context.Background(), 101, LXCNetUpdateOptions{
		Node:   "pve1",
		Device: "net0",
		Bridge: "vmbr0",
	})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "not a bridge") {
		t.Fatalf("err = %v, want a not-a-bridge refusal", err)
	}
}
