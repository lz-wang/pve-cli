package pve

import (
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pvectl/internal/output"
)

// nodeDetail maps /nodes/{node}/status onto the stable NodeDetail contract.
// A node that answers the status endpoint is online by definition.
func nodeDetail(node *proxmox.Node) output.NodeDetail {
	if node == nil {
		return output.NodeDetail{}
	}
	return output.NodeDetail{
		Name:          node.Name,
		Status:        "online",
		CPU:           node.CPU,
		Mem:           node.Memory.Used,
		MaxMem:        node.Memory.Total,
		Disk:          node.RootFS.Used,
		MaxDisk:       node.RootFS.Total,
		Uptime:        node.Uptime,
		PVEVersion:    node.PVEVersion,
		KernelVersion: node.Kversion,
		LoadAverage:   strings.Join(node.LoadAvg, " "),
		CPUModel:      node.CPUInfo.Model,
		CPUCores:      node.CPUInfo.Cores,
		CPUSockets:    node.CPUInfo.Sockets,
	}
}

func nodeRow(node *proxmox.NodeStatus) output.NodeRow {
	if node == nil {
		return output.NodeRow{}
	}
	name := node.Node
	if name == "" {
		name = node.Name
	}
	status := node.Status
	if status == "" && node.Online == 1 {
		status = "online"
	}
	return output.NodeRow{
		Name:    name,
		Status:  status,
		CPU:     node.CPU,
		Mem:     node.Mem,
		MaxMem:  node.MaxMem,
		Disk:    node.Disk,
		MaxDisk: node.MaxDisk,
		Uptime:  node.Uptime,
	}
}

func vmRow(vm *proxmox.VirtualMachine) output.GuestRow {
	if vm == nil {
		return output.GuestRow{Kind: "vm"}
	}
	return output.GuestRow{
		Kind:    "vm",
		VMID:    uint64(vm.VMID),
		Name:    vm.Name,
		Node:    vm.Node,
		Status:  vm.Status,
		CPUs:    vm.CPUs,
		CPU:     vm.CPU,
		Mem:     vm.Mem,
		MaxMem:  vm.MaxMem,
		MaxDisk: vm.MaxDisk,
		Uptime:  vm.Uptime,
		Tags:    vm.Tags,
	}
}

func lxcRow(ct *proxmox.Container) output.GuestRow {
	if ct == nil {
		return output.GuestRow{Kind: "lxc"}
	}
	return output.GuestRow{
		Kind:    "lxc",
		VMID:    uint64(ct.VMID),
		Name:    ct.Name,
		Node:    ct.Node,
		Status:  ct.Status,
		CPUs:    ct.CPUs,
		Mem:     0,
		MaxMem:  ct.MaxMem,
		MaxDisk: ct.MaxDisk,
		Uptime:  ct.Uptime,
		Tags:    ct.Tags,
	}
}

func storageRow(storage *proxmox.Storage) output.StorageRow {
	if storage == nil {
		return output.StorageRow{}
	}
	return output.StorageRow{
		Node:         storage.Node,
		Storage:      storage.Name,
		Type:         storage.Type,
		Active:       storage.Active == 1,
		Enabled:      storage.Enabled == 1,
		Shared:       storage.Shared == 1,
		Content:      storage.Content,
		Used:         storage.Used,
		Avail:        storage.Avail,
		Total:        storage.Total,
		UsedFraction: storage.UsedFraction,
	}
}
