package pve

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pve-cli/v3/internal/config"
)

type ClientOptions struct {
	TokenSecret string
	Timeout     time.Duration
	Insecure    bool
}

func NewProxmoxBackend(profile config.Profile, opts ClientOptions) (Backend, error) {
	if profile.Endpoint == "" {
		return nil, fmt.Errorf("pve endpoint is empty")
	}
	if profile.TokenID == "" {
		return nil, fmt.Errorf("pve token_id is empty")
	}
	if opts.TokenSecret == "" {
		return nil, fmt.Errorf("pve token secret is empty")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: opts.Insecure || profile.InsecureSkipVerify}
	httpClient := &http.Client{Timeout: opts.Timeout, Transport: statusTransport{transport}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	clientOpts := []proxmox.Option{proxmox.WithAPIToken(profile.TokenID, opts.TokenSecret), proxmox.WithHTTPClient(httpClient)}

	return &ProxmoxBackend{client: proxmox.NewClient(profile.Endpoint, clientOpts...)}, nil
}
