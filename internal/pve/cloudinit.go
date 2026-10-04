package pve

import (
	"context"
	"fmt"
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pvectl/internal/output"
)

// maxCloudInitDevices bounds how many ipconfigN devices are mapped into the
// normalized view. PVE supports ipconfig0..ipconfig31 on qemu NICs.
const maxCloudInitDevices = 32

// CloudInitBackend covers PVE-native cloud-init configuration for VMs.
// It depends on PVE's own cloud-init plumbing; pvectl never builds ISOs.
type CloudInitBackend interface {
	GuestBackend
	VirtualMachineCloudInit(ctx context.Context, node string, vmid int) (output.CloudInitConfig, error)
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
