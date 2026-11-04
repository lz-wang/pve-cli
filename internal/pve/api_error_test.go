package pve

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/config"
)

func apiErrorTestTransport(t *testing.T, code int, body string) (statusTransport, *http.Request) {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPut, "https://pve.invalid:8006/api2/json/nodes/n/lxc/101/config", nil)
	return statusTransport{base: fixedTransport{resp: &http.Response{
		StatusCode: code,
		Status:     fmt.Sprintf("%d %s", code, http.StatusText(code)),
		Body:       newBytesBody(body),
		Header:     http.Header{},
		Request:    req,
	}}, redact: newErrorSanitizer("private-test-token")}, req
}

func TestAPIErrorClassifiesHTTPStatus(t *testing.T) {
	cases := []struct {
		code     int
		kind     error
		category string
	}{
		{400, ErrInvalid, "invalid"},
		{401, ErrPermissionDenied, "permission_denied"},
		{403, ErrPermissionDenied, "permission_denied"},
		{404, ErrNotFound, "not_found"},
		{409, ErrConflict, "conflict"},
		{422, ErrInvalid, "invalid"},
		{500, ErrUnavailable, "unavailable"},
		{503, ErrUnavailable, "unavailable"},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.code), func(t *testing.T) {
			transport, req := apiErrorTestTransport(t, tc.code, `{"message":"server rejected the request"}`)
			_, err := transport.RoundTrip(req)
			if !errors.Is(err, tc.kind) {
				t.Fatalf("kind = %v, want %v", err, tc.kind)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected *APIError, got %T", err)
			}
			if apiErr.HTTPStatus != tc.code {
				t.Fatalf("http_status = %d, want %d", apiErr.HTTPStatus, tc.code)
			}
			if apiErr.Category != tc.category {
				t.Fatalf("category = %q, want %q", apiErr.Category, tc.category)
			}
			if apiErr.Operation != "PUT /nodes/n/lxc/101/config" {
				t.Fatalf("operation = %q", apiErr.Operation)
			}
			if apiErr.Resource != "lxc" {
				t.Fatalf("resource = %q, want lxc", apiErr.Resource)
			}
			if apiErr.Node != "n" {
				t.Fatalf("node = %q, want n", apiErr.Node)
			}
			if apiErr.Phase != PhaseRequest {
				t.Fatalf("phase = %q, want %q", apiErr.Phase, PhaseRequest)
			}
			if apiErr.Message != "server rejected the request" {
				t.Fatalf("message not preserved: %q", apiErr.Message)
			}
		})
	}
}

// A 500 whose parsed body message states the resource does not exist is a
// reliable not-found signal; the status-line description text is not consulted.
func TestAPIError500MissingResourceFromBodyMessage(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://pve.invalid:8006/api2/json/nodes/n/qemu/999/status/current", nil)
	transport := statusTransport{base: fixedTransport{resp: &http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
		Body:       newBytesBody(`{"data":null,"message":"CT 999 does not exist"}`),
		Header:     http.Header{},
		Request:    req,
	}}, redact: newErrorSanitizer("private-test-token")}

	_, err := transport.RoundTrip(req)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("kind = %v, want not found", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if !strings.Contains(apiErr.Message, "does not exist") {
		t.Fatalf("message = %q", apiErr.Message)
	}
	if apiErr.Resource != "qemu" {
		t.Fatalf("resource = %q", apiErr.Resource)
	}
}

// The legacy branch classified via the HTTP status-line description text; it
// must be gone. A 500 with a not-found status line but an unrelated body
// stays unavailable.
func TestStatusTransportIgnoresStatusLineText(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://pve.invalid:8006/api2/json/nodes/n/qemu/999/status/current", nil)
	transport := statusTransport{base: fixedTransport{resp: &http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     "500 CT 999 does not exist",
		Body:       newBytesBody(`{"message":"internal cluster error"}`),
		Header:     http.Header{},
		Request:    req,
	}}, redact: newErrorSanitizer("private-test-token")}
	_, err := transport.RoundTrip(req)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("kind = %v, want unavailable", err)
	}
}

func TestAPIErrorPreservesSanitizedFieldErrors(t *testing.T) {
	transport, req := apiErrorTestTransport(t, http.StatusBadRequest,
		`{"data":null,"message":"parameter verification failed",`+
			`"errors":{"net0":"invalid bridge name","password":"echoed-secret-value"}}`)

	_, err := transport.RoundTrip(req)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if got := apiErr.FieldErrors["net0"]; got != "invalid bridge name" {
		t.Fatalf("net0 = %q", got)
	}
	if got := apiErr.FieldErrors["password"]; got != "[REDACTED]" {
		t.Fatalf("password = %q, want [REDACTED]", got)
	}
	if strings.Contains(err.Error(), "echoed-secret") || strings.Contains(apiErr.Message, "echoed-secret") {
		t.Fatalf("echoed credential leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "net0") || !strings.Contains(err.Error(), "invalid bridge name") {
		t.Fatalf("field error missing from output: %v", err)
	}
}

// P0-4: parameter names that carry whole credential bundles (target-endpoint,
// apitoken) must have their echoed values redacted as a whole, even when no
// exact secret is registered for the sanitizer.
func TestAPIErrorRedactsEndpointBundleFieldsWhole(t *testing.T) {
	bundle := "apitoken=PVEAPIToken=root@pam!tgt=unregistered-secret-value,host=10.0.0.2,port=8006"
	transport, req := apiErrorTestTransport(t, http.StatusBadRequest,
		`{"data":null,"message":"parameter verification failed",`+
			`"errors":{"target-endpoint":"`+bundle+`"}}`)

	_, err := transport.RoundTrip(req)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if got := apiErr.FieldErrors["target-endpoint"]; got != "[REDACTED]" {
		t.Fatalf("target-endpoint = %q, want [REDACTED]", got)
	}
	if strings.Contains(err.Error(), "unregistered-secret-value") {
		t.Fatal("unregistered bundle secret leaked into error output")
	}
}

func TestAPIErrorNonJSONBodyIsBounded(t *testing.T) {
	for name, body := range map[string]string{
		"html":      "<html><body>502 Bad Gateway nginx</body></html>",
		"empty":     "",
		"huge":      strings.Repeat("A", 1<<20),
		"truncated": `{"message":"never ends`,
		"deep":      `{"errors":{"k":["a","b"]}}`,
		"scalar":    `"plain text"`,
	} {
		t.Run(name, func(t *testing.T) {
			transport, req := apiErrorTestTransport(t, http.StatusBadGateway, body)
			_, err := transport.RoundTrip(req)
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("kind = %v", err)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected *APIError, got %T", err)
			}
			if len(apiErr.Message) > sanitizedMessageLimit {
				t.Fatalf("message exceeds limit: %d", len(apiErr.Message))
			}
		})
	}
}

func TestAPIErrorRedactsCredentials(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://pve.invalid:8006/api2/json/nodes/n/lxc", nil)
	transport := statusTransport{base: fixedTransport{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Status:     "400 Bad Request",
		Body: newBytesBody(`{"message":"token ` + secret + ` rejected, ` +
			`also see https://root:Hunter2@example.invalid:8006 and api_key=ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789, ` +
			`password: my-password-value"}`),
		Header:  http.Header{},
		Request: req,
	}}, redact: newErrorSanitizer(secret)}

	_, err := transport.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error")
	}
	rendered := err.Error()
	for _, leak := range []string{secret, "Hunter2", "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", "my-password-value"} {
		if strings.Contains(rendered, leak) {
			t.Fatalf("credential %q leaked: %v", leak, rendered)
		}
	}
	var apiErr *APIError
	errors.As(err, &apiErr)
	if strings.Contains(apiErr.Message, secret) || strings.Contains(apiErr.Message, "Hunter2") {
		t.Fatalf("credential leaked into message: %q", apiErr.Message)
	}
}

func TestAPIErrorTruncatesLargeFieldSets(t *testing.T) {
	fields := make([]string, 32)
	for i := range fields {
		fields[i] = fmt.Sprintf(`"flag%d":"%s"`, i, strings.Repeat("x", 1024))
	}
	transport, req := apiErrorTestTransport(t, http.StatusBadRequest,
		`{"message":"parameter verification failed","errors":{`+strings.Join(fields, ",")+`}}`)

	_, err := transport.RoundTrip(req)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if len(apiErr.FieldErrors) > maxFieldErrors {
		t.Fatalf("field errors not capped: %d", len(apiErr.FieldErrors))
	}
	for name, msg := range apiErr.FieldErrors {
		if len(msg) > sanitizedFieldLimit {
			t.Fatalf("field %q exceeds limit: %d", name, len(msg))
		}
	}
}

func TestAPIErrorMessageMentionsOperationAndStatus(t *testing.T) {
	transport, req := apiErrorTestTransport(t, http.StatusForbidden, `{"message":"no permission"}`)

	_, err := transport.RoundTrip(req)
	text := err.Error()
	for _, want := range []string{"403", "PUT /nodes/n/lxc/101/config", "no permission", ErrPermissionDenied.Error()} {
		if !strings.Contains(text, want) {
			t.Fatalf("error text %q missing %q", text, want)
		}
	}
}

// The sanitized PVE details must survive the full backend call path so
// commands and exit codes keep working against a real HTTP server.
func TestAPIErrorSurvivesBackendCallPath(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"data":null,"message":"parameter verification failed","errors":{"storage":"storage 'x' does not exist"}}`))
	}))
	defer server.Close()
	backend, err := NewProxmoxBackend(config.Profile{Endpoint: server.URL, TokenID: "test@pve!cli"}, ClientOptions{TokenSecret: "private-test-token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = backend.(GuestCreator).CreateGuest(context.Background(), "lxc", "n", 201, map[string]any{})
	if !errors.Is(err, ErrInvalid) || calls != 1 {
		t.Fatalf("err = %v calls = %d", err, calls)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("field error lost on path: %v", err)
	}
	if strings.Contains(err.Error(), "private-test-token") {
		t.Fatalf("token secret leaked: %v", err)
	}
}

type fixedTransport struct{ resp *http.Response }

func (f fixedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return f.resp, nil
}

type bytesBody struct {
	data string
	pos  int
}

func newBytesBody(data string) *bytesBody { return &bytesBody{data: data} }

func (b *bytesBody) Read(p []byte) (int, error) {
	if b.pos >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.pos:])
	b.pos += n
	return n, nil
}

func (b *bytesBody) Close() error { return nil }
