package pve

import (
	"context"
	"fmt"
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pvectl/internal/output"
)

// NodeBackend lists cluster nodes and returns node detail. It is embedded by
// the other capability interfaces because per-node queries need node
// discovery.
type NodeBackend interface {
	Nodes(ctx context.Context) ([]output.NodeRow, error)
	Node(ctx context.Context, name string) (output.NodeDetail, error)
}

// GuestBackend covers VM/QEMU and LXC guest queries plus node discovery.
type GuestBackend interface {
	NodeBackend
	VMs(ctx context.Context, node string) ([]output.GuestRow, error)
	VM(ctx context.Context, node string, vmid int) (Guest, error)
	LXCs(ctx context.Context, node string) ([]output.GuestRow, error)
	LXC(ctx context.Context, node string, vmid int) (Guest, error)
}

// BackupBackend covers backup listing, one-off backup trigger, and the guest
// resolution needed to locate a guest before backing it up.
type BackupBackend interface {
	GuestBackend
	Backups(ctx context.Context, node, storage string) ([]output.BackupRow, error)
	BackupGuest(ctx context.Context, node string, options BackupOptions) (Task, error)
}

// StorageBackend covers storage inventory plus node discovery.
type StorageBackend interface {
	NodeBackend
	Storages(ctx context.Context, node string) ([]output.StorageRow, error)
	Storage(ctx context.Context, node, storage string) (output.StorageRow, error)
	StorageContents(ctx context.Context, node, storage string) ([]output.StorageContentRow, error)
}

// Backend composes every capability. Services should depend on the smallest
// capability interface they need instead of the full Backend.
type Backend interface {
	NodeBackend
	GuestBackend
	BackupBackend
	StorageBackend
	TaskBackend
	RestoreBackend
	AgentBackend
}

type ProxmoxBackend struct {
	client *proxmox.Client
}

func (b *ProxmoxBackend) Nodes(ctx context.Context) ([]output.NodeRow, error) {
	nodes, err := b.client.Nodes(ctx)
	if err != nil {
		return nil, err
	}

	rows := make([]output.NodeRow, 0, len(nodes))
	for _, node := range nodes {
		rows = append(rows, nodeRow(node))
	}
	return rows, nil
}

func (b *ProxmoxBackend) Node(ctx context.Context, nodeName string) (output.NodeDetail, error) {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return output.NodeDetail{}, fmt.Errorf("node is required")
	}
	node, err := b.client.Node(ctx, nodeName)
	if err != nil {
		return output.NodeDetail{}, err
	}
	if err := node.Status(ctx); err != nil {
		return output.NodeDetail{}, err
	}
	return nodeDetail(node), nil
}

func (b *ProxmoxBackend) VMs(ctx context.Context, nodeName string) ([]output.GuestRow, error) {
	node, err := b.client.Node(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	vms, err := node.VirtualMachines(ctx)
	if err != nil {
		return nil, err
	}

	rows := make([]output.GuestRow, 0, len(vms))
	for _, vm := range vms {
		rows = append(rows, vmRow(vm))
	}
	return rows, nil
}

func (b *ProxmoxBackend) VM(ctx context.Context, nodeName string, vmid int) (Guest, error) {
	node, err := b.client.Node(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	vm, err := node.VirtualMachine(ctx, vmid)
	if err != nil {
		return nil, err
	}
	return vmGuest{vm: vm}, nil
}

func (b *ProxmoxBackend) LXCs(ctx context.Context, nodeName string) ([]output.GuestRow, error) {
	node, err := b.client.Node(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	containers, err := node.Containers(ctx)
	if err != nil {
		return nil, err
	}

	rows := make([]output.GuestRow, 0, len(containers))
	for _, ct := range containers {
		rows = append(rows, lxcRow(ct))
	}
	return rows, nil
}

func (b *ProxmoxBackend) LXC(ctx context.Context, nodeName string, vmid int) (Guest, error) {
	node, err := b.client.Node(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	ct, err := node.Container(ctx, vmid)
	if err != nil {
		return nil, err
	}
	return lxcGuest{ct: ct}, nil
}
