package pve

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// NativeConfigBackend retrieves native dynamic configuration fields.
type NativeConfigBackend interface {
	GuestConfig(context.Context, string, string, int) (map[string]any, error)
}

func guestPath(kind, node string, id int) string {
	apiKind := "qemu"
	if kind == "lxc" {
		apiKind = "lxc"
	}
	return fmt.Sprintf("/nodes/%s/%s/%d", url.PathEscape(node), apiKind, id)
}
func (b *ProxmoxBackend) GuestConfig(ctx context.Context, kind, node string, id int) (map[string]any, error) {
	var cfg map[string]any
	err := b.client.Get(ctx, guestPath(kind, node, id)+"/config", &cfg)
	return cfg, ClassifyError(err)
}

// NativeConfig returns a redacted copy, preserving noncredential native keys.
func (s *GuestService) NativeConfig(ctx context.Context, id int, node string) (map[string]any, error) {
	guest, err := s.resolve(ctx, id, node)
	if err != nil {
		return nil, err
	}
	backend, ok := s.backend.(NativeConfigBackend)
	if !ok {
		return nil, fmt.Errorf("%w: native config unavailable", ErrUnavailable)
	}
	cfg, err := backend.GuestConfig(ctx, s.kind, guest.Row().Node, id)
	if err != nil {
		return nil, err
	}
	result := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if sensitiveConfigKey(k) {
			v = "[REDACTED]"
		}
		result[k] = v
	}
	return result, nil
}
func sensitiveConfigKey(k string) bool {
	k = strings.ToLower(k)
	return strings.Contains(k, "password") || strings.Contains(k, "secret") || strings.Contains(k, "token") || strings.Contains(k, "privatekey")
}
