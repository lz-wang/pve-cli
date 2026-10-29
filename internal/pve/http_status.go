package pve

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// statusTransport adds typed status errors around the SDK's single HTTP client.
// Error bodies are never printed: PVE may echo submitted credential values.
type statusTransport struct{ base http.RoundTripper }

func (t statusTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, ClassifyError(err)
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response, nil
	}
	if response.Body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
	}
	kind := ErrUnavailable
	switch response.StatusCode {
	case 400, 422:
		kind = ErrInvalid
	case 401, 403:
		kind = ErrPermissionDenied
	case 404:
		kind = ErrNotFound
	case 409:
		kind = ErrConflict
	}
	if response.StatusCode == 500 && strings.Contains(strings.ToLower(response.Status), "does not exist") {
		kind = ErrNotFound
	}
	return nil, fmt.Errorf("%w: PVE HTTP %d %s", kind, response.StatusCode, http.StatusText(response.StatusCode))
}
