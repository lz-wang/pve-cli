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

// verifyingBackend extends the shared fake with a native config read so the
// write-verification path can be exercised.
type verifyingBackend struct {
	*fakeBackend
	config      map[string]any
	configErr   error
	configCalls int
}

func (b *verifyingBackend) GuestConfig(context.Context, string, string, int) (map[string]any, error) {
	b.configCalls++
	if b.configErr != nil {
		return nil, b.configErr
	}
	return b.config, nil
}

func newVerifyService(t *testing.T, guest *fakeGuest, backend *verifyingBackend, wait bool) (*GuestService, *bytes.Buffer) {
	t.Helper()
	backend.lxcs = map[string]map[int]*fakeGuest{"pve1": {101: guest}}
	backend.nodes = []output.NodeRow{{Name: "pve1", Status: "online"}}
	var out bytes.Buffer
	runner := TaskRunner{Wait: wait, WaitTimeout: time.Second, Format: "json", ResultWriter: &out}
	return NewLXCService(backend, runner, nil, false), &out
}

func decodeOperation(t *testing.T, data string) output.OperationResult {
	t.Helper()
	var result output.OperationResult
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		t.Fatalf("invalid operation json %q: %v", data, err)
	}
	return result
}

// PUT returns HTTP 500 but the GET read shows the expected value already in
// effect: the result reports verified=true together with the original API
// error instead of pretending nothing happened.
func TestConfigVerifyHTTP500ButValueApplied(t *testing.T) {
	guest := &fakeGuest{row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve1"}, configErr: &APIError{
		HTTPStatus: 500, Category: CategoryUnavailable, Message: "internal error", Operation: "PUT /nodes/pve1/lxc/101/config", kind: ErrUnavailable,
	}}
	backend := &verifyingBackend{fakeBackend: &fakeBackend{}, config: map[string]any{"cores": float64(2)}}
	svc, out := newVerifyService(t, guest, backend, false)

	err := svc.Config(context.Background(), 101, "pve1", map[string]string{"cores": "2"})
	if err != nil {
		t.Fatalf("verified write must not fail the command: %v", err)
	}
	result := decodeOperation(t, out.String())
	if result.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if result.Verified == nil || !*result.Verified {
		t.Fatalf("verified = %v, want true", result.Verified)
	}
	if !strings.Contains(result.APIError, "500") || !strings.Contains(result.APIError, "internal error") {
		t.Fatalf("api_error not preserved: %q", result.APIError)
	}
	if backend.configCalls != 1 || guest.configCalls != 1 {
		t.Fatalf("submit calls = %d, read calls = %d; mutation must not be retried", guest.configCalls, backend.configCalls)
	}
}

// PUT fails and the value is not in effect: the original error surfaces and
// the result is marked unverified/failed without any retry.
func TestConfigVerifyHTTP500ValueNotApplied(t *testing.T) {
	guest := &fakeGuest{row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve1"}, configErr: &APIError{
		HTTPStatus: 500, Category: CategoryUnavailable, Message: "internal error", kind: ErrUnavailable,
	}}
	backend := &verifyingBackend{fakeBackend: &fakeBackend{}, config: map[string]any{"cores": float64(1)}}
	svc, out := newVerifyService(t, guest, backend, false)

	err := svc.Config(context.Background(), 101, "pve1", map[string]string{"cores": "2"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want unavailable", err)
	}
	result := decodeOperation(t, out.String())
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if result.Verified == nil || *result.Verified {
		t.Fatalf("verified = %v, want false", result.Verified)
	}
	if guest.configCalls != 1 || backend.configCalls != 1 {
		t.Fatalf("retried: submit = %d reads = %d", guest.configCalls, backend.configCalls)
	}
}

// PUT times out and the follow-up read fails: outcome stays unknown, the
// recoverable task error is returned, nothing is retried.
func TestConfigVerifyPUTTimeoutReadFails(t *testing.T) {
	guest := &fakeGuest{
		row:  output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve1"},
		task: &fakeTask{upid: "UPID:pve1:config", waitErr: context.DeadlineExceeded},
	}
	backend := &verifyingBackend{fakeBackend: &fakeBackend{}, configErr: fmt.Errorf("%w: read failed", ErrUnavailable)}
	svc, out := newVerifyService(t, guest, backend, true)

	err := svc.Config(context.Background(), 101, "pve1", map[string]string{"cores": "2"})
	if !errors.Is(err, ErrTaskTimeout) {
		t.Fatalf("err = %v, want task timeout", err)
	}
	result := decodeOperation(t, out.String())
	if result.Status != "unknown" {
		t.Fatalf("status = %q, want unknown", result.Status)
	}
	if result.Verified != nil {
		t.Fatalf("verified = %v, want nil", result.Verified)
	}
	if result.Task != "UPID:pve1:config" {
		t.Fatalf("task = %q, want preserved UPID", result.Task)
	}
	if guest.configCalls != 1 || backend.configCalls != 1 {
		t.Fatalf("retried: submit = %d reads = %d", guest.configCalls, backend.configCalls)
	}
}

// Synchronous write that reports success but reads back differently is a
// detected mismatch, not a silent success.
func TestConfigVerifySyncWriteMismatch(t *testing.T) {
	guest := &fakeGuest{row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve1"}}
	backend := &verifyingBackend{fakeBackend: &fakeBackend{}, config: map[string]any{"cores": float64(1)}}
	svc, out := newVerifyService(t, guest, backend, false)

	err := svc.Config(context.Background(), 101, "pve1", map[string]string{"cores": "2"})
	if err == nil || !strings.Contains(err.Error(), "cores") {
		t.Fatalf("err = %v, want mismatch naming cores", err)
	}
	result := decodeOperation(t, out.String())
	if result.Status != "failed" || result.Verified == nil || *result.Verified {
		t.Fatalf("result = %+v, want failed/unverified", result)
	}
	if !strings.Contains(result.Error, "cores") {
		t.Fatalf("error = %q, want mismatch detail", result.Error)
	}
}

// Numeric strings compare canonically: "0" equals numeric 0, and option
// strings match when every requested attribute is present with the same
// value even if PVE appended attributes (hwaddr).
func TestConfigVerifyValueNormalization(t *testing.T) {
	cases := []struct {
		name       string
		values     map[string]string
		config     map[string]any
		verified   bool
		comparable bool
	}{
		{name: "zero string vs zero number", values: map[string]string{"cores": "0"}, config: map[string]any{"cores": float64(0)}, verified: true, comparable: true},
		{name: "plain number", values: map[string]string{"memory": "512"}, config: map[string]any{"memory": float64(512)}, verified: true, comparable: true},
		{name: "number echoed as string", values: map[string]string{"cores": "2"}, config: map[string]any{"cores": "2"}, verified: true, comparable: true},
		{
			name:     "option string with appended attributes",
			values:   map[string]string{"net0": "name=eth0,bridge=vmbr0"},
			config:   map[string]any{"net0": "name=eth0,bridge=vmbr0,hwaddr=BC:24:11:2F:AB:12"},
			verified: true, comparable: true,
		},
		{
			name:     "option string different attribute value",
			values:   map[string]string{"net0": "name=eth0,bridge=vmbr1"},
			config:   map[string]any{"net0": "name=eth0,bridge=vmbr0"},
			verified: false, comparable: true,
		},
		{name: "absent key is not a false mismatch", values: map[string]string{"onboot": "1"}, config: map[string]any{}, verified: false, comparable: false},
		{name: "structured value not comparable", values: map[string]string{"sshkeys": "key"}, config: map[string]any{"sshkeys": []any{"key"}}, verified: false, comparable: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var requested string
			for _, v := range tc.values {
				requested = v
				break
			}
			var current any
			for _, v := range tc.config {
				current = v
				break
			}
			match, comparable := configValueMatches(requested, current)
			if comparable != tc.comparable || (comparable && match != tc.verified) {
				t.Fatalf("match=%v comparable=%v, want match=%v comparable=%v", match, comparable, tc.verified, tc.comparable)
			}
		})
	}
}

func TestConfigVerifyEndToEndNormalization(t *testing.T) {
	guest := &fakeGuest{row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve1"}}
	backend := &verifyingBackend{fakeBackend: &fakeBackend{}, config: map[string]any{
		"cores": float64(0),
		"net0":  "name=eth0,bridge=vmbr0,hwaddr=BC:24:11:2F:AB:12",
	}}
	svc, out := newVerifyService(t, guest, backend, false)

	err := svc.Config(context.Background(), 101, "pve1", map[string]string{"cores": "0", "net0": "name=eth0,bridge=vmbr0"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	result := decodeOperation(t, out.String())
	if result.Verified == nil || !*result.Verified {
		t.Fatalf("verified = %v, want true (result %s)", result.Verified, out.String())
	}
}

// Credential fields are transformed on store (hashed), so they are skipped
// by verification and their submitted values are never echoed anywhere.
func TestConfigVerifySkipsAndNeverEchoesSensitiveKeys(t *testing.T) {
	guest := &fakeGuest{row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve1"}}
	backend := &verifyingBackend{fakeBackend: &fakeBackend{}, config: map[string]any{"cipassword": "$5$hash-of-hunter2-secret"}}
	svc, out := newVerifyService(t, guest, backend, false)

	err := svc.Config(context.Background(), 101, "pve1", map[string]string{"cipassword": "hunter2-secret"})
	if err != nil {
		t.Fatalf("err = %v, want unverifiable-not-failed", err)
	}
	if strings.Contains(out.String(), "hunter2-secret") {
		t.Fatalf("credential leaked: %s", out.String())
	}
	result := decodeOperation(t, out.String())
	if result.Verified != nil {
		t.Fatalf("verified = %v, want nil for hashed credential", result.Verified)
	}
}

// Backends without native config reads keep the legacy behavior: no
// verification fields, original statuses.
func TestConfigVerifySkippedWithoutConfigBackend(t *testing.T) {
	guest := &fakeGuest{row: output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve1"}}
	backend := &fakeBackend{}
	backend.lxcs = map[string]map[int]*fakeGuest{"pve1": {101: guest}}
	var out bytes.Buffer
	svc := NewLXCService(backend, TaskRunner{Format: "json", ResultWriter: &out}, nil, false)

	if err := svc.Config(context.Background(), 101, "pve1", map[string]string{"cores": "2"}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(out.String(), "verified") {
		t.Fatalf("verified leaked without config backend: %s", out.String())
	}
}

// Task wait timeout followed by a successful read that shows the expected
// values upgrades the outcome to verified instead of staying unknown.
func TestConfigVerifyTimeoutThenConfirmed(t *testing.T) {
	guest := &fakeGuest{
		row:  output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve1"},
		task: &fakeTask{upid: "UPID:pve1:config", waitErr: context.DeadlineExceeded},
	}
	backend := &verifyingBackend{fakeBackend: &fakeBackend{}, config: map[string]any{"cores": float64(2)}}
	svc, out := newVerifyService(t, guest, backend, true)

	err := svc.Config(context.Background(), 101, "pve1", map[string]string{"cores": "2"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	result := decodeOperation(t, out.String())
	if result.Status != "succeeded" || result.Verified == nil || !*result.Verified {
		t.Fatalf("result = %+v, want succeeded/verified", result)
	}
}

// Async writes without --wait stay submitted; no premature verification.
func TestConfigVerifyAsyncSubmittedNotVerified(t *testing.T) {
	guest := &fakeGuest{
		row:  output.GuestRow{Kind: "lxc", VMID: 101, Node: "pve1"},
		task: &fakeTask{upid: "UPID:pve1:config"},
	}
	backend := &verifyingBackend{fakeBackend: &fakeBackend{}}
	svc, out := newVerifyService(t, guest, backend, false)

	if err := svc.Config(context.Background(), 101, "pve1", map[string]string{"cores": "2"}); err != nil {
		t.Fatalf("err = %v", err)
	}
	result := decodeOperation(t, out.String())
	if result.Status != "submitted" {
		t.Fatalf("status = %q, want submitted", result.Status)
	}
	if backend.configCalls != 0 {
		t.Fatalf("premature verification read: %d", backend.configCalls)
	}
}

// The full HTTP path against a real transport: a 500 on PUT whose follow-up
// GET shows the requested value yields succeeded + verified + api_error.
func TestConfigVerifyHTTPContract500ThenApplied(t *testing.T) {
	putCalls, getCalls := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/lxc/101/config"):
			putCalls++
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"data":null,"message":"internal error after commit"}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/lxc/101/config"):
			getCalls++
			_, _ = w.Write([]byte(`{"data":{"cores":2}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/lxc"):
			_, _ = w.Write([]byte(`{"data":[{"vmid":101,"status":"stopped","name":"ct101","lock":"","type":"lxc"}]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/lxc/101/status/current"):
			_, _ = w.Write([]byte(`{"data":{"vmid":101,"status":"stopped","name":"ct101"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/status"):
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
	runner := TaskRunner{Format: "json", ResultWriter: &out}
	svc := NewLXCService(backend, runner, nil, false)

	// resolve() walks nodes -> lxc list; supply a node list first.
	err = svc.Config(context.Background(), 101, "pve1", map[string]string{"cores": "2"})
	if err != nil {
		t.Fatalf("verified write must not fail the command: %v", err)
	}
	result := decodeOperation(t, out.String())
	if result.Status != "succeeded" || result.Verified == nil || !*result.Verified {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.APIError, "internal error after commit") {
		t.Fatalf("api_error = %q", result.APIError)
	}
	// One PUT (no mutation retry) and exactly two config reads: the SDK
	// guest resolution read plus the verification read.
	if putCalls != 1 || getCalls != 2 {
		t.Fatalf("put = %d get = %d; must not retry", putCalls, getCalls)
	}
}
