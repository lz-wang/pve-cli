package pve

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// Error phases recorded on APIError so callers can tell where a failure was
// observed: the HTTP request itself, waiting for an async task, or a
// post-mutation state verification read.
const (
	PhaseRequest  = "request"
	PhaseTaskWait = "task_wait"
	PhaseVerify   = "verify"
)

// Semantic category labels carried by APIError.Category.
const (
	CategoryInvalid          = "invalid"
	CategoryPermissionDenied = "permission_denied"
	CategoryNotFound         = "not_found"
	CategoryConflict         = "conflict"
	CategoryUnavailable      = "unavailable"
)

// Bounds for sanitized error details. Raw bodies are never surfaced; only
// individually checked and truncated fields are.
const (
	errorBodyReadLimit    = 16 * 1024
	sanitizedMessageLimit = 512
	sanitizedFieldLimit   = 256
	maxFieldErrors        = 16
	redactedPlaceholder   = "[REDACTED]"
)

// APIError is the structured PVE API failure. It preserves the HTTP status,
// sanitized server message, field-level validation errors, and the operation
// context (operation/resource/node/phase) while wrapping the semantic error
// kind so errors.Is keeps driving exit codes.
type APIError struct {
	HTTPStatus  int               `json:"http_status"`
	Category    string            `json:"category"`
	Message     string            `json:"message,omitempty"`
	FieldErrors map[string]string `json:"field_errors,omitempty"`
	Operation   string            `json:"operation,omitempty"`
	Resource    string            `json:"resource,omitempty"`
	Node        string            `json:"node,omitempty"`
	Phase       string            `json:"phase,omitempty"`

	kind error
}

func (e *APIError) Error() string {
	var b strings.Builder
	b.WriteString(e.kind.Error())
	if e.Operation != "" {
		b.WriteString(": ")
		b.WriteString(e.Operation)
	}
	fmt.Fprintf(&b, ": PVE HTTP %d %s", e.HTTPStatus, http.StatusText(e.HTTPStatus))
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	names := make([]string, 0, len(e.FieldErrors))
	for name := range e.FieldErrors {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&b, " (%s: %s)", name, e.FieldErrors[name])
	}
	return b.String()
}

// Unwrap exposes the semantic kind sentinel (ErrInvalid, ErrNotFound, ...).
func (e *APIError) Unwrap() error { return e.kind }

// WithContext returns a copy of the error with additional operation context.
// It is used by higher layers that know more than the transport does, for
// example the verification phase after a configuration write.
func (e *APIError) WithContext(operation, resource, node, phase string) *APIError {
	clone := *e
	if operation != "" {
		clone.Operation = operation
	}
	if resource != "" {
		clone.Resource = resource
	}
	if node != "" {
		clone.Node = node
	}
	if phase != "" {
		clone.Phase = phase
	}
	return &clone
}

func categoryForKind(kind error) string {
	switch kind {
	case ErrInvalid:
		return CategoryInvalid
	case ErrPermissionDenied:
		return CategoryPermissionDenied
	case ErrNotFound:
		return CategoryNotFound
	case ErrConflict:
		return CategoryConflict
	case ErrUnavailable:
		return CategoryUnavailable
	default:
		return ""
	}
}

// kindForHTTPStatus maps an HTTP status onto the CLI semantic kind. Only the
// numeric status and the parsed response body decide the category; the HTTP
// status-line description text is never consulted.
func kindForHTTPStatus(status int) error {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return ErrInvalid
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrPermissionDenied
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict, http.StatusPreconditionFailed:
		// 412 is PVE's optimistic-concurrency digest mismatch.
		return ErrConflict
	default:
		return ErrUnavailable
	}
}

// notFoundBodyPatterns classify 5xx bodies whose server message reliably
// states a missing resource. PVE reports several lookup failures as 500.
var notFoundBodyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bdoes not exist\b`),
	regexp.MustCompile(`(?i)\bno such (guest|vm|container|ct|storage|volume|task|node)\b`),
	regexp.MustCompile(`(?i)\bnot found\b`),
}

func messageIndicatesNotFound(message string) bool {
	for _, pattern := range notFoundBodyPatterns {
		if pattern.MatchString(message) {
			return true
		}
	}
	return false
}

// errorSanitizer removes credentials from PVE-provided error text before it
// is stored on APIError. PVE may echo submitted values (including token
// secrets and guest credentials) inside validation messages.
type errorSanitizer struct {
	exact []string
}

func newErrorSanitizer(secrets ...string) *errorSanitizer {
	s := &errorSanitizer{}
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if len(secret) >= 6 {
			s.exact = append(s.exact, secret)
		}
	}
	return s
}

var (
	credentialAssignment = regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api[_-]?key|authorization|auth)\b\s*[=:]\s*("[^"]*"|'[^']*'|[^\s,;)]+)`)
	urlUserInfo          = regexp.MustCompile(`(?i)(https?://[^/@\s:]+):([^/@\s]+)@`)
	longOpaqueToken      = regexp.MustCompile(`[A-Za-z0-9+/_=-]{40,}`)
	controlAndSpace      = regexp.MustCompile(`[\x00-\x08\x0b-\x1f\x7f]`)
)

func (s *errorSanitizer) text(value string, limit int) string {
	if value == "" {
		return ""
	}
	for _, secret := range s.exact {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, redactedPlaceholder)
		}
	}
	value = credentialAssignment.ReplaceAllString(value, "${1}="+redactedPlaceholder)
	value = urlUserInfo.ReplaceAllString(value, "${1}:"+redactedPlaceholder+"@")
	value = longOpaqueToken.ReplaceAllString(value, redactedPlaceholder)
	value = controlAndSpace.ReplaceAllString(value, " ")
	value = strings.TrimSpace(value)
	if len(value) > limit {
		value = value[:limit] + "…[truncated]"
	}
	return value
}

// sanitizeFieldName reports whether a field-level validation error may echo
// the submitted credential value itself; such messages are redacted whole.
// Endpoint parameters carry whole token bundles, so they are always treated
// as sensitive regardless of the registered secret list.
func sanitizeFieldName(name string) bool {
	name = strings.ToLower(name)
	return sensitiveConfigKey(name) || strings.Contains(name, "endpoint")
}

// parsePVEErrorBody decodes a bounded slice of a PVE error body. Unknown
// shapes yield empty message and fields without failing.
func parsePVEErrorBody(body []byte) (message string, fieldErrors map[string]string) {
	var parsed struct {
		Message string         `json:"message"`
		Errors  map[string]any `json:"errors"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", nil
	}
	if len(parsed.Errors) > 0 {
		fieldErrors = make(map[string]string, len(parsed.Errors))
		for name, value := range parsed.Errors {
			fieldErrors[name] = fmt.Sprint(value)
		}
	}
	return parsed.Message, fieldErrors
}

// newAPIError builds the structured transport error for a non-2xx response.
// The body is drained (bounded) so the connection can be reused, but only
// sanitized fields survive.
func newAPIError(response *http.Response, redact *errorSanitizer) *APIError {
	if redact == nil {
		redact = &errorSanitizer{}
	}
	kind := kindForHTTPStatus(response.StatusCode)

	var raw []byte
	if response.Body != nil {
		raw, _ = io.ReadAll(io.LimitReader(response.Body, errorBodyReadLimit))
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		_ = response.Body.Close()
	}

	message, fieldErrors := parsePVEErrorBody(raw)
	message = redact.text(message, sanitizedMessageLimit)
	if response.StatusCode >= 500 && messageIndicatesNotFound(message) {
		kind = ErrNotFound
	}

	sanitizedFields := make(map[string]string, len(fieldErrors))
	names := make([]string, 0, len(fieldErrors))
	for name := range fieldErrors {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > maxFieldErrors {
		names = names[:maxFieldErrors]
	}
	for _, name := range names {
		cleanName := redact.text(name, 128)
		if cleanName == "" {
			continue
		}
		if sanitizeFieldName(name) {
			sanitizedFields[cleanName] = redactedPlaceholder
			continue
		}
		sanitizedFields[cleanName] = redact.text(fieldErrors[name], sanitizedFieldLimit)
	}

	apiErr := &APIError{
		HTTPStatus:  response.StatusCode,
		Category:    categoryForKind(kind),
		Message:     message,
		FieldErrors: sanitizedFields,
		Phase:       PhaseRequest,
		kind:        kind,
	}
	if response.Request != nil {
		apiErr.Operation = operationLabel(response.Request)
		apiErr.Resource, apiErr.Node = resourceFromPath(response.Request.URL.Path)
	}
	return apiErr
}

func operationLabel(request *http.Request) string {
	if request == nil || request.URL == nil {
		return ""
	}
	path := request.URL.Path
	// Drop the API prefix so the operation reads like the PVE API reference.
	for _, prefix := range []string{"/api2/json", "/api2/extjs"} {
		if trimmed, found := strings.CutPrefix(path, prefix); found {
			path = trimmed
			break
		}
	}
	return request.Method + " " + path
}

// resourceFromPath extracts the guest resource kind and node from a PVE API
// path such as /api2/json/nodes/pve1/lxc/101/config.
func resourceFromPath(path string) (resource, node string) {
	segments := strings.Split(strings.SplitN(path, "?", 2)[0], "/")
	const nodesToken = "nodes"
	for i := 0; i < len(segments)-1; i++ {
		if segments[i] != nodesToken {
			continue
		}
		node = segments[i+1]
		if i+2 < len(segments) {
			switch segments[i+2] {
			case "qemu", "lxc":
				resource = segments[i+2]
			case "storage", "storages":
				resource = "storage"
			case "network":
				resource = "network"
			default:
				resource = segments[i+2]
			}
		}
		break
	}
	return resource, node
}
