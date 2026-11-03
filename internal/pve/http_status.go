package pve

import (
	"net/http"
)

// statusTransport adds typed status errors around the SDK's single HTTP
// client. Non-2xx responses are converted into *APIError: the bounded body is
// parsed and only sanitized fields survive, because PVE may echo submitted
// credential values inside error messages.
type statusTransport struct {
	base   http.RoundTripper
	redact *errorSanitizer
}

func (t statusTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, ClassifyError(err)
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response, nil
	}
	return nil, newAPIError(response, t.redact)
}
