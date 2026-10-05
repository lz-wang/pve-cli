package pve

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

// maxCloudInitDevices bounds how many ipconfigN devices are mapped into the
// normalized view. PVE supports ipconfig0..ipconfig31 on qemu NICs.
const maxCloudInitDevices = 32

// CloudInitBackend covers PVE-native cloud-init configuration for VMs.
// It depends on PVE's own cloud-init plumbing; pve never builds ISOs.
type CloudInitBackend interface {
	GuestBackend
	VirtualMachineCloudInit(ctx context.Context, node string, vmid int) (output.CloudInitConfig, error)
	RegenerateVirtualMachineCloudInit(ctx context.Context, node string, vmid int) error
}

// VirtualMachineCloudInit returns the normalized cloud-init view of a VM
// config. The cipassword value is deliberately dropped.
func (b *ProxmoxBackend) VirtualMachineCloudInit(ctx context.Context, nodeName string, vmid int) (output.CloudInitConfig, error) {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return output.CloudInitConfig{}, fmt.Errorf("node is required")
	}
	if vmid <= 0 {
		return output.CloudInitConfig{}, fmt.Errorf("invalid vmid %d", vmid)
	}
	node, err := b.client.Node(ctx, nodeName)
	if err != nil {
		return output.CloudInitConfig{}, err
	}
	vm, err := node.VirtualMachine(ctx, vmid)
	if err != nil {
		return output.CloudInitConfig{}, err
	}
	// Ping refreshes both status and the config document in one call.
	if err := vm.Ping(ctx); err != nil {
		return output.CloudInitConfig{}, err
	}
	return cloudInitConfig(vm), nil
}

// RegenerateVirtualMachineCloudInit rewrites the cloud-init image so the next
// guest boot picks up pending changes. The PVE endpoint is synchronous.
func (b *ProxmoxBackend) RegenerateVirtualMachineCloudInit(ctx context.Context, nodeName string, vmid int) error {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return fmt.Errorf("node is required")
	}
	if vmid <= 0 {
		return fmt.Errorf("invalid vmid %d", vmid)
	}
	node, err := b.client.Node(ctx, nodeName)
	if err != nil {
		return err
	}
	vm, err := node.VirtualMachine(ctx, vmid)
	if err != nil {
		return err
	}
	return vm.CloudInitRegenerate(ctx)
}

// cloudInitConfig maps the upstream config onto the stable output contract.
func cloudInitConfig(vm *proxmox.VirtualMachine) output.CloudInitConfig {
	if vm == nil {
		return output.CloudInitConfig{}
	}
	cfg := vm.VirtualMachineConfig
	out := output.CloudInitConfig{
		VMID:               uint64(vm.VMID),
		Node:               vm.Node,
		User:               cfg.CIUser,
		PasswordConfigured: strings.TrimSpace(cfg.CIPassword) != "",
		SSHKeys:            cfg.SSHKeys,
		Nameserver:         cfg.Nameserver,
		SearchDomain:       cfg.Searchdomain,
		Type:               cfg.CIType,
	}

	for i := 0; i < maxCloudInitDevices; i++ {
		device := fmt.Sprintf("ipconfig%d", i)
		if value, ok := cfg.IPConfigs[device]; ok {
			out.IPConfigs = append(out.IPConfigs, output.CloudInitIPConfig{Device: device, Config: value})
		}
	}
	out.Custom = parseCloudInitCustom(cfg.CICustom)
	return out
}

// parseCloudInitCustom splits cicustom ("user=vol,network=vol") into pairs.
func parseCloudInitCustom(raw string) []output.CloudInitCustom {
	var custom []output.CloudInitCustom
	for _, part := range strings.Split(raw, ",") {
		device, volume, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found || strings.TrimSpace(device) == "" || strings.TrimSpace(volume) == "" {
			continue
		}
		custom = append(custom, output.CloudInitCustom{Device: device, Volume: volume})
	}
	return custom
}

// CloudInitSetOptions describes one `vm cloud-init set` request. The password
// travels through an environment variable; pve never takes it as a flag.
type CloudInitSetOptions struct {
	User         string
	PasswordEnv  string
	SSHKeyFile   string
	IPConfigs    map[string]string
	Nameserver   string
	SearchDomain string
}

// CloudInitService reads and updates PVE-native cloud-init settings on VMs.
type CloudInitService struct {
	backend CloudInitBackend
	tasks   TaskRunner
	logger  *slog.Logger
	verbose bool
}

func NewCloudInitService(backend CloudInitBackend, tasks TaskRunner, logger *slog.Logger, verbose bool) *CloudInitService {
	return &CloudInitService{backend: backend, tasks: tasks, logger: logger, verbose: verbose}
}

// Get returns the normalized cloud-init config for a VM.
func (s *CloudInitService) Get(ctx context.Context, vmid int, node string) (output.CloudInitConfig, error) {
	if vmid <= 0 {
		return output.CloudInitConfig{}, fmt.Errorf("invalid vmid %d", vmid)
	}
	node, err := s.resolveNode(ctx, vmid, node)
	if err != nil {
		return output.CloudInitConfig{}, err
	}
	return s.backend.VirtualMachineCloudInit(ctx, node, vmid)
}

// Set maps the requested options onto PVE cloud-init keys and updates the VM
// config through the regular config path.
func (s *CloudInitService) Set(ctx context.Context, vmid int, node string, options CloudInitSetOptions) error {
	values, err := cloudInitValues(options)
	if err != nil {
		return err
	}
	return NewVMService(s.backend, s.tasks, s.logger, s.verbose).Config(ctx, vmid, node, values)
}

// Regenerate rewrites the cloud-init image. The PVE endpoint runs
// synchronously, so there is no task to wait on.
func (s *CloudInitService) Regenerate(ctx context.Context, vmid int, node string) error {
	if vmid <= 0 {
		return fmt.Errorf("invalid vmid %d", vmid)
	}
	node, err := s.resolveNode(ctx, vmid, node)
	if err != nil {
		return err
	}
	return s.backend.RegenerateVirtualMachineCloudInit(ctx, node, vmid)
}

// resolveNode keeps --node optional for the direct backend calls, matching
// `vm cloud-init set`, which already resolves through the VM config path.
func (s *CloudInitService) resolveNode(ctx context.Context, vmid int, node string) (string, error) {
	node = strings.TrimSpace(node)
	if node != "" {
		return node, nil
	}
	row, err := NewVMService(s.backend, TaskRunner{}, s.logger, s.verbose).Get(ctx, vmid, "")
	if err != nil {
		return "", err
	}
	return row.Node, nil
}

func cloudInitValues(options CloudInitSetOptions) (map[string]string, error) {
	values := make(map[string]string)
	if strings.TrimSpace(options.User) != "" {
		values["ciuser"] = options.User
	}
	if strings.TrimSpace(options.PasswordEnv) != "" {
		secret := os.Getenv(options.PasswordEnv)
		if strings.TrimSpace(secret) == "" {
			return nil, fmt.Errorf("environment variable %s is not set", options.PasswordEnv)
		}
		values["cipassword"] = secret
	}
	if strings.TrimSpace(options.SSHKeyFile) != "" {
		data, err := os.ReadFile(options.SSHKeyFile)
		if err != nil {
			return nil, fmt.Errorf("read ssh key file: %w", err)
		}
		keys := parseSSHKeyLines(string(data))
		if len(keys) == 0 {
			return nil, fmt.Errorf("ssh key file %s contains no public keys", options.SSHKeyFile)
		}
		values["sshkeys"] = proxmox.EncodeSSHKeys(keys...)
	}
	for device, config := range options.IPConfigs {
		device = strings.ToLower(strings.TrimSpace(device))
		config = strings.TrimSpace(config)
		if device == "" || config == "" {
			return nil, fmt.Errorf("ipconfig device and value are required")
		}
		if !regexp.MustCompile(`^ipconfig\d+$`).MatchString(device) {
			return nil, fmt.Errorf("invalid ipconfig device %q", device)
		}
		values[device] = config
	}
	if strings.TrimSpace(options.Nameserver) != "" {
		values["nameserver"] = options.Nameserver
	}
	if strings.TrimSpace(options.SearchDomain) != "" {
		values["searchdomain"] = options.SearchDomain
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one cloud-init option is required")
	}
	return values, nil
}

// parseSSHKeyLines returns one entry per non-empty, non-comment line. An
// OpenSSH public key is "type base64 comment" on a single line, so splitting
// on whitespace would shred every key into fragments and corrupt the
// cloud-init sshkeys config.
func parseSSHKeyLines(data string) []string {
	var keys []string
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		keys = append(keys, line)
	}
	return keys
}
