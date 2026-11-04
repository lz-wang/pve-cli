package pve

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// LXCNetUpdateOptions describes a partial update of one LXC network device.
// Only the attributes explicitly set are modified; every other attribute of
// the device (name, ip, gw, hwaddr, firewall, ...) is preserved.
type LXCNetUpdateOptions struct {
	Node    string
	Device  string // net0, net1, ...
	Bridge  string // bridge=vmbrX
	Name    string // name=eth0
	IP      string // ip=dhcp or ip=cidr
	Gateway string // gw=address
	Tag     string // tag=VLAN id
	Rate    string // rate=mbps
}

var lxcNetDevicePattern = regexp.MustCompile(`^net[0-9]+$`)

// mergeNetAttributes applies attribute updates to a PVE option string,
// replacing existing attributes in place and appending new ones at the end.
// Original ordering and unrecognized segments are preserved.
func mergeNetAttributes(current string, updates map[string]string) string {
	segments := strings.Split(current, ",")
	applied := make(map[string]bool, len(updates))
	for i, segment := range segments {
		name, _, found := strings.Cut(strings.TrimSpace(segment), "=")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		if value, ok := updates[name]; ok {
			segments[i] = name + "=" + value
			applied[name] = true
		}
	}
	// Deterministic append order keeps submissions stable.
	names := make([]string, 0, len(updates))
	for name := range updates {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if applied[name] {
			continue
		}
		segments = append(segments, name+"="+updates[name])
	}
	return strings.Join(segments, ",")
}

// UpdateLXCNet performs a safe partial update of one LXC network device:
// read the complete device configuration, modify only the requested
// attributes, verify the target bridge exists on the node, and submit the
// complete value together with the configuration digest so concurrent
// modifications fail as conflicts instead of overwriting each other.
func (s *GuestService) UpdateLXCNet(ctx context.Context, vmid int, options LXCNetUpdateOptions) error {
	if s.kind != "lxc" {
		return fmt.Errorf("%w: net updates are only supported for lxc guests", ErrInvalid)
	}
	device := strings.TrimSpace(options.Device)
	if device == "" {
		return fmt.Errorf("%w: --device is required (net0, net1, ...)", ErrInvalid)
	}
	if !lxcNetDevicePattern.MatchString(device) {
		return fmt.Errorf("%w: invalid device %q, expected net0..netN", ErrInvalid, device)
	}
	updates, err := netAttributeUpdates(options)
	if err != nil {
		return err
	}
	if len(updates) == 0 {
		return fmt.Errorf("%w: at least one attribute (--bridge, --name, --ip, --gw, --tag, --rate) is required", ErrInvalid)
	}
	if vmid <= 0 {
		return fmt.Errorf("%w: invalid vmid %d", ErrInvalid, vmid)
	}

	configBackend, ok := s.backend.(NativeConfigBackend)
	if !ok {
		return fmt.Errorf("%w: native config unavailable", ErrUnavailable)
	}
	guest, err := s.resolve(ctx, vmid, options.Node)
	if err != nil {
		return err
	}
	guestNode := guest.Row().Node

	config, err := configBackend.GuestConfig(ctx, s.kind, guestNode, vmid)
	if err != nil {
		return ClassifyError(err)
	}
	current, ok := config[device].(string)
	if !ok || strings.TrimSpace(current) == "" {
		return fmt.Errorf("%w: device %s is not configured on guest %d", ErrNotFound, device, vmid)
	}

	if bridge, ok := updates["bridge"]; ok && bridge != "" {
		if err := s.checkBridgeExists(ctx, guestNode, bridge); err != nil {
			return err
		}
	}

	merged := mergeNetAttributes(current, updates)
	submitValues := map[string]string{device: merged}
	if digest, ok := config["digest"].(string); ok && digest != "" {
		// Optimistic concurrency: PVE rejects the write with 412 when the
		// configuration changed since it was read.
		submitValues["digest"] = digest
	}

	return s.submitAndVerify(ctx, guest, vmid, guestNode, submitValues, map[string]string{device: merged})
}

func netAttributeUpdates(options LXCNetUpdateOptions) (map[string]string, error) {
	updates := make(map[string]string)
	for name, value := range map[string]string{
		"bridge": options.Bridge,
		"name":   options.Name,
		"ip":     options.IP,
		"gw":     options.Gateway,
		"tag":    options.Tag,
		"rate":   options.Rate,
	} {
		value = strings.TrimSpace(value)
		if value != "" {
			updates[name] = value
		}
	}
	if tag, ok := updates["tag"]; ok {
		if _, err := strconv.Atoi(tag); err != nil {
			return nil, fmt.Errorf("%w: invalid VLAN tag %q, expected an integer", ErrInvalid, tag)
		}
	}
	return updates, nil
}

// checkBridgeExists fails with ErrInvalid when the target bridge is not
// present on the node, or when the name belongs to a non-bridge interface —
// a name match alone would route the device onto an ordinary link.
func (s *GuestService) checkBridgeExists(ctx context.Context, node, bridge string) error {
	networks, ok := s.backend.(NetworkBackend)
	if !ok {
		return nil // capability-only backend: skip the check
	}
	rows, err := networks.Networks(ctx, node, NetworkListOptions{})
	if err != nil {
		return ClassifyError(err)
	}
	for _, row := range rows {
		if row.Name != bridge {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(row.Type), "bridge") {
			return fmt.Errorf("%w: %q on node %s is a %s, not a bridge", ErrInvalid, bridge, node, row.Type)
		}
		return nil
	}
	return fmt.Errorf("%w: bridge %q does not exist on node %s", ErrInvalid, bridge, node)
}
