package pve

import (
	"context"
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
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
	for _, key := range []string{"ip", "ip6"} {
		v := netValues[key]
		if v != "" && v != "dhcp" && v != "manual" && v != "auto" {
			if _, _, e := net.ParseCIDR(v); e != nil {
				return result, fmt.Errorf("%w: invalid %s", ErrInvalid, key)
			}
		}
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
			if !strings.HasPrefix(line, "ssh-") && !strings.HasPrefix(line, "ecdsa-") && !strings.HasPrefix(line, "sk-") {
				return result, fmt.Errorf("%w: SSH key file must contain public keys", ErrInvalid)
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
	apiKind := "qemu"
	if kind == "lxc" {
		apiKind = "lxc"
	}
	body := make(map[string]any, len(params)+1)
	for k, v := range params {
		body[k] = v
	}
	body["vmid"] = id
	var upid string
	if err := b.client.Post(ctx, "/nodes/"+url.PathEscape(node)+"/"+apiKind, body, &upid); err != nil {
		return nil, ClassifyError(err)
	}
	return b.TaskHandle(upid)
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
