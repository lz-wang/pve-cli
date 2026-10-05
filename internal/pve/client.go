package pve

import (
	"fmt"
	"time"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pve-cli/v2/internal/config"
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

	clientOpts := []proxmox.Option{
		proxmox.WithAPIToken(profile.TokenID, opts.TokenSecret),
		proxmox.WithTimeout(opts.Timeout),
		proxmox.WithRetry(),
	}
	if opts.Insecure || profile.InsecureSkipVerify {
		clientOpts = append(clientOpts, proxmox.WithInsecureSkipVerify())
	}

	return &ProxmoxBackend{client: proxmox.NewClient(profile.Endpoint, clientOpts...)}, nil
}
