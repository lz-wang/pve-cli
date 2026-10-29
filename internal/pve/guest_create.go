package pve

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	proxmox "github.com/luthermonson/go-proxmox"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"math"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// CreateBackend is the read capability required for fail-closed preflight.
type CreateBackend interface {
	GuestBackend
	StorageBackend
	NetworkBackend
}
type GuestCreator interface {
	CreateGuest(context.Context, string, string, int, map[string]any) (Task, error)
}
type CreateService struct {
	backend CreateBackend
	tasks   TaskRunner
}

func NewCreateService(b CreateBackend, t TaskRunner) *CreateService { return &CreateService{b, t} }

type LXCCreateOptions struct {
	ID                                                        int
	Node, Template, RootFS, Hostname, Net0, SSHKeys, Password string
	Memory, Cores, Swap                                       int
	Unprivileged                                              bool
}

var resourceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
var hostnamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*$`)

func (s *CreateService) preflight(ctx context.Context, id int, node string) error {
	if id < 100 || id > 999999999 || !resourceNamePattern.MatchString(node) {
		return fmt.Errorf("%w: ID must be 100..999999999 and node is required", ErrInvalid)
	}
	n, err := s.backend.Node(ctx, node)
	if err != nil {
		return ClassifyError(err)
	}
	if n.Status != "online" {
		return fmt.Errorf("%w: node is not online", ErrUnavailable)
	}
	if b, ok := s.backend.(ClusterGuestBackend); ok {
		rows, err := b.ClusterGuests(ctx)
		if err != nil {
			return ClassifyError(err)
		}
		for _, r := range rows {
			if int(r.VMID) == id {
				return fmt.Errorf("%w: VMID %d already occupied", ErrConflict, id)
			}
		}
		return nil
	}
	nodes, err := s.backend.Nodes(ctx)
	if err != nil {
		return ClassifyError(err)
	}
	for _, n := range nodes {
		vms, err := s.backend.VMs(ctx, n.Name)
		if err != nil {
			return ClassifyError(err)
		}
		lxcs, err := s.backend.LXCs(ctx, n.Name)
		if err != nil {
			return ClassifyError(err)
		}
		for _, r := range append(vms, lxcs...) {
			if int(r.VMID) == id {
				return fmt.Errorf("%w: VMID %d already occupied", ErrConflict, id)
			}
		}
	}
	return nil
}
func (s *CreateService) storage(ctx context.Context, node, name, content string) error {
	if !resourceNamePattern.MatchString(name) {
		return fmt.Errorf("%w: invalid storage", ErrInvalid)
	}
	r, err := s.backend.Storage(ctx, node, name)
	if err != nil {
		return ClassifyError(err)
	}
	if !storageSupports(r, content) {
		return fmt.Errorf("%w: storage %s does not support active %s content", ErrInvalid, name, content)
	}
	return nil
}
func (s *CreateService) volume(ctx context.Context, node, vol, content string) error {
	parts := strings.SplitN(vol, ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return fmt.Errorf("%w: complete storage volume required", ErrInvalid)
	}
	if err := s.storage(ctx, node, parts[0], content); err != nil {
		return err
	}
	rows, err := s.backend.StorageContents(ctx, node, parts[0])
	if err != nil {
		return ClassifyError(err)
	}
	for _, r := range rows {
		if r.VolID == vol && r.Content == content {
			return nil
		}
	}
	return fmt.Errorf("%w: volume %s is not readable", ErrNotFound, vol)
}
func (s *CreateService) bridge(ctx context.Context, node, bridge string) error {
	if !resourceNamePattern.MatchString(bridge) {
		return fmt.Errorf("%w: invalid network bridge", ErrInvalid)
	}
	rows, err := s.backend.Networks(ctx, node, NetworkListOptions{})
	if err != nil {
		return ClassifyError(err)
	}
	for _, r := range rows {
		if r.Name == bridge && r.Type == "bridge" {
			return nil
		}
	}
	return fmt.Errorf("%w: bridge %s", ErrNotFound, bridge)
}
func (s *CreateService) CreateLXC(ctx context.Context, o LXCCreateOptions) (output.OperationResult, error) {
	result := output.OperationResult{Resource: "lxc", VMID: o.ID, Node: o.Node, Status: "unknown"}
	if o.Memory < 16 || o.Cores < 1 || o.Swap < 0 || (o.Hostname != "" && !hostnamePattern.MatchString(o.Hostname)) {
		return result, fmt.Errorf("%w: invalid LXC memory, cores, swap or hostname", ErrInvalid)
	}
	root := strings.Split(o.RootFS, ":")
	if len(root) != 2 || !regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`).MatchString(root[1]) {
		return result, fmt.Errorf("%w: rootfs must be STORAGE:GiB", ErrInvalid)
	}
	size, _ := strconv.ParseFloat(root[1], 64)
	if size <= 0 {
		return result, fmt.Errorf("%w: rootfs size must be positive", ErrInvalid)
	}
	netValues, err := parseProperty(o.Net0, map[string]bool{"name": true, "bridge": true, "ip": true, "ip6": true, "gw": true, "gw6": true, "hwaddr": true, "tag": true, "firewall": true, "mtu": true, "rate": true, "type": true})
	if err != nil {
		return result, err
	}
	if o.Net0 != "" && netValues["name"] == "" {
		return result, fmt.Errorf("%w: net0 name is required", ErrInvalid)
	}
	if err = validateLXCNetwork(netValues); err != nil {
		return result, err
	}

	if err = s.preflight(ctx, o.ID, o.Node); err != nil {
		return result, err
	}
	if err = s.volume(ctx, o.Node, o.Template, "vztmpl"); err != nil {
		return result, err
	}
	if err = s.storage(ctx, o.Node, root[0], "rootdir"); err != nil {
		return result, err
	}
	if bridge := netValues["bridge"]; bridge != "" {
		if err = s.bridge(ctx, o.Node, bridge); err != nil {
			return result, err
		}
	}
	if o.SSHKeys != "" {
		for _, line := range strings.Split(strings.TrimSpace(o.SSHKeys), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 || (!strings.HasPrefix(fields[0], "ssh-") && !strings.HasPrefix(fields[0], "ecdsa-") && !strings.HasPrefix(fields[0], "sk-")) {
				return result, fmt.Errorf("%w: SSH key file must contain public keys", ErrInvalid)
			}
			decoded, e := base64.StdEncoding.DecodeString(fields[1])
			if e != nil || len(decoded) < 4 {
				return result, fmt.Errorf("%w: invalid SSH public key", ErrInvalid)
			}
			length := int(binary.BigEndian.Uint32(decoded[:4]))
			if length > len(decoded)-4 || string(decoded[4:4+length]) != fields[0] {
				return result, fmt.Errorf("%w: invalid SSH public key type", ErrInvalid)
			}
		}
	}

	params := map[string]any{"ostemplate": o.Template, "rootfs": o.RootFS, "memory": o.Memory, "cores": o.Cores, "swap": o.Swap, "unprivileged": o.Unprivileged, "start": false}
	if o.Hostname != "" {
		params["hostname"] = o.Hostname
	}
	if o.Net0 != "" {
		params["net0"] = o.Net0
	}
	if o.SSHKeys != "" {
		params["ssh-public-keys"] = url.QueryEscape(o.SSHKeys)
	}
	if o.Password != "" {
		params["password"] = o.Password
	}
	result, err = s.submit(ctx, "lxc", o.Node, o.ID, params)
	if err != nil && o.Password != "" {
		err = fmt.Errorf("%w", redactError(err, o.Password))
	}
	return result, err
}
func parseProperty(value string, allowed map[string]bool) (map[string]string, error) {
	values := map[string]string{}
	if value == "" {
		return values, nil
	}
	for _, part := range strings.Split(value, ",") {
		pair := strings.SplitN(part, "=", 2)
		if len(pair) != 2 || !allowed[pair[0]] || pair[1] == "" || strings.ContainsAny(pair[1], "\n\r\x00") {
			return nil, fmt.Errorf("%w: invalid network property", ErrInvalid)
		}
		if _, exists := values[pair[0]]; exists {
			return nil, fmt.Errorf("%w: duplicate network property", ErrInvalid)
		}
		values[pair[0]] = pair[1]
	}
	return values, nil
}
func (s *CreateService) submit(ctx context.Context, kind, node string, id int, params map[string]any) (output.OperationResult, error) {
	r := output.OperationResult{Resource: kind, VMID: id, Node: node, Status: "unknown"}
	creator, ok := s.backend.(GuestCreator)
	if !ok {
		return r, fmt.Errorf("%w: creation unavailable", ErrUnavailable)
	}
	task, err := creator.CreateGuest(ctx, kind, node, id, params)
	if err != nil {
		return r, ClassifyError(err)
	}
	runner := s.tasks
	runner.Resource = kind
	runner.VMID = id
	runner.Node = node
	return runner.Execute(ctx, task)
}
func (b *ProxmoxBackend) CreateGuest(ctx context.Context, kind, node string, id int, params map[string]any) (Task, error) {
	sdkNode := (&proxmox.Node{}).New(b.client, node)
	var task *proxmox.Task
	var err error
	if kind == "lxc" {
		options := make([]proxmox.ContainerOption, 0, len(params))
		for k, v := range params {
			options = append(options, proxmox.ContainerOption{Name: k, Value: v})
		}
		task, err = sdkNode.NewContainer(ctx, id, options...)
	} else {
		options := make([]proxmox.VirtualMachineOption, 0, len(params))
		for k, v := range params {
			options = append(options, proxmox.VirtualMachineOption{Name: k, Value: v})
		}
		task, err = sdkNode.NewVirtualMachine(ctx, id, options...)
	}
	if err != nil {
		return nil, ClassifyError(err)
	}
	if task == nil {
		return nil, fmt.Errorf("%w: creation accepted but no UPID returned", ErrTaskTimeout)
	}
	return wrapTask(task), nil
}

// redactedError retains the error chain for exit-code classification.
type redactedError struct {
	cause   error
	message string
}

func (e redactedError) Error() string { return e.message }
func (e redactedError) Unwrap() error { return e.cause }
func redactError(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	return redactedError{err, strings.ReplaceAll(err.Error(), secret, "[REDACTED]")}
}

// VMCreateOptions configures a stopped VM, with an optional installer ISO.
type VMCreateOptions struct {
	ID                                         int
	Node, Name, Storage, DiskSize, Bridge, ISO string
	Memory, Cores                              int
	Onboot                                     bool
}

var diskSizePattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?G$`)

func diskGiB(value string) (string, error) {
	if !diskSizePattern.MatchString(value) {
		return "", fmt.Errorf("%w: disk size must be positive GiB, for example 32G", ErrInvalid)
	}
	gib := strings.TrimSuffix(value, "G")
	n, err := strconv.ParseFloat(gib, 64)
	if err != nil || n <= 0 {
		return "", fmt.Errorf("%w: disk size must be positive", ErrInvalid)
	}
	return gib, nil
}
func (s *CreateService) CreateVM(ctx context.Context, o VMCreateOptions) (output.OperationResult, error) {
	result := output.OperationResult{Resource: "vm", VMID: o.ID, Node: o.Node, Status: "unknown"}
	size, err := diskGiB(o.DiskSize)
	if err != nil {
		return result, err
	}
	if o.Memory < 16 || o.Cores < 1 || (o.Name != "" && !hostnamePattern.MatchString(o.Name)) {
		return result, fmt.Errorf("%w: invalid VM memory, cores or name", ErrInvalid)
	}
	// Validate every interpolated property before any mutation.
	if !resourceNamePattern.MatchString(o.Storage) || !resourceNamePattern.MatchString(o.Bridge) || strings.ContainsAny(o.ISO, ",=\r\n\x00") {
		return result, fmt.Errorf("%w: invalid storage, bridge or ISO volume", ErrInvalid)
	}
	if err = s.preflight(ctx, o.ID, o.Node); err != nil {
		return result, err
	}
	if err = s.storage(ctx, o.Node, o.Storage, "images"); err != nil {
		return result, err
	}
	if err = s.bridge(ctx, o.Node, o.Bridge); err != nil {
		return result, err
	}
	if o.ISO != "" {
		if err = s.volume(ctx, o.Node, o.ISO, "iso"); err != nil {
			return result, err
		}
	}
	params := map[string]any{"memory": o.Memory, "cores": o.Cores, "scsihw": "virtio-scsi-single", "scsi0": o.Storage + ":" + size, "net0": "virtio,bridge=" + o.Bridge, "onboot": o.Onboot, "start": false, "boot": "order=scsi0;net0"}
	if o.Name != "" {
		params["name"] = o.Name
	}
	if o.ISO != "" {
		params["ide2"] = o.ISO + ",media=cdrom"
		params["boot"] = "order=ide2;scsi0;net0"
	}
	return s.submit(ctx, "vm", o.Node, o.ID, params)
}

func validateLXCNetwork(v map[string]string) error {
	invalid := func(key string) error { return fmt.Errorf("%w: invalid net0 %s", ErrInvalid, key) }
	for _, key := range []string{"name", "bridge"} {
		if value := v[key]; value != "" && (!resourceNamePattern.MatchString(value) || len(value) > 15) {
			return invalid(key)
		}
	}
	for _, key := range []string{"ip", "ip6"} {
		value := v[key]
		if value == "" || value == "manual" || value == "dhcp" || (key == "ip6" && value == "auto") {
			continue
		}
		ip, _, err := net.ParseCIDR(value)
		if err != nil || (key == "ip" && ip.To4() == nil) || (key == "ip6" && ip.To4() != nil) {
			return invalid(key)
		}
	}
	for _, key := range []string{"gw", "gw6"} {
		if value := v[key]; value != "" {
			ip := net.ParseIP(value)
			if ip == nil || (key == "gw" && ip.To4() == nil) || (key == "gw6" && ip.To4() != nil) {
				return invalid(key)
			}
		}
	}
	if value := v["hwaddr"]; value != "" {
		mac, err := net.ParseMAC(value)
		if err != nil || len(mac) != 6 {
			return invalid("hwaddr")
		}
	}
	for key, bounds := range map[string][2]int{"tag": {1, 4094}, "mtu": {64, 65535}} {
		if value := v[key]; value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < bounds[0] || n > bounds[1] {
				return invalid(key)
			}
		}
	}
	if value := v["firewall"]; value != "" && value != "0" && value != "1" {
		return invalid("firewall")
	}
	if value := v["type"]; value != "" && value != "veth" {
		return invalid("type")
	}
	if value := v["rate"]; value != "" {
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) {
			return invalid("rate")
		}
	}
	return nil
}
