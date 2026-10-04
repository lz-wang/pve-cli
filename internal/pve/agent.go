package pve

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pvectl/internal/output"
)

// DefaultAgentExecTimeout bounds how long AgentExec waits for the guest
// command to exit.
const DefaultAgentExecTimeout = 30 * time.Second

// AgentBackend covers read-mostly QEMU guest agent operations for VMs only.
// LXC containers do not expose the QEMU agent API.
type AgentBackend interface {
	GuestBackend
	AgentPing(ctx context.Context, node string, vmid int) error
	AgentNetwork(ctx context.Context, node string, vmid int) ([]output.AgentNetworkRow, error)
	AgentExec(ctx context.Context, node string, vmid int, options AgentExecOptions) (output.AgentExecResult, error)
}

// AgentExecOptions describes one guest agent exec request. Command is
// executable + argv; pvectl never wraps it in a shell implicitly.
type AgentExecOptions struct {
	Command []string
	Input   string
	Timeout time.Duration
}

// AgentPing verifies the QEMU guest agent answers on a VM.
func (b *ProxmoxBackend) AgentPing(ctx context.Context, nodeName string, vmid int) error {
	vm, err := b.agentVM(ctx, nodeName, vmid)
	if err != nil {
		return err
	}
	return vm.AgentPing(ctx)
}

// AgentNetwork returns normalized network interface rows from the agent.
func (b *ProxmoxBackend) AgentNetwork(ctx context.Context, nodeName string, vmid int) ([]output.AgentNetworkRow, error) {
	vm, err := b.agentVM(ctx, nodeName, vmid)
	if err != nil {
		return nil, err
	}
	ifaces, err := vm.AgentGetNetworkIFaces(ctx)
	if err != nil {
		return nil, err
	}

	rows := make([]output.AgentNetworkRow, 0, len(ifaces))
	for _, iface := range ifaces {
		if iface == nil {
			continue
		}
		row := output.AgentNetworkRow{
			Name:            iface.Name,
			HardwareAddress: iface.HardwareAddress,
		}
		for _, addr := range iface.IPAddresses {
			if addr == nil || addr.IPAddress == "" {
				continue
			}
			row.Addresses = append(row.Addresses, fmt.Sprintf("%s/%d", addr.IPAddress, addr.Prefix))
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// AgentExec runs executable+argv inside the guest and waits for exit. The
// exit code of the guest command is preserved in the result; a non-zero exit
// is not a transport error.
func (b *ProxmoxBackend) AgentExec(ctx context.Context, nodeName string, vmid int, options AgentExecOptions) (output.AgentExecResult, error) {
	if len(options.Command) == 0 || strings.TrimSpace(options.Command[0]) == "" {
		return output.AgentExecResult{}, fmt.Errorf("agent exec requires an executable path, for example -- /usr/bin/uname -a")
	}
	vm, err := b.agentVM(ctx, nodeName, vmid)
	if err != nil {
		return output.AgentExecResult{}, err
	}

	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultAgentExecTimeout
	}
	pid, err := vm.AgentExec(ctx, options.Command, options.Input)
	if err != nil {
		return output.AgentExecResult{}, err
	}

	seconds := int(math.Ceil(timeout.Seconds()))
	if seconds <= 0 {
		seconds = 30
	}
	status, err := vm.WaitForAgentExecExit(ctx, pid, seconds)
	if err != nil {
		if errors.Is(err, proxmox.ErrTimeout) {
			return output.AgentExecResult{}, fmt.Errorf("agent exec timed out after %s", timeout)
		}
		return output.AgentExecResult{}, err
	}

	result := output.AgentExecResult{
		ExitCode:  status.ExitCode,
		Stdout:    status.OutData,
		Stderr:    status.ErrData,
		Truncated: bool(status.OutTruncated) || status.ErrTruncated,
	}
	if status.Signal {
		result.Signal = 1
	}
	return result, nil
}

func (b *ProxmoxBackend) agentVM(ctx context.Context, nodeName string, vmid int) (*proxmox.VirtualMachine, error) {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return nil, fmt.Errorf("node is required")
	}
	if vmid <= 0 {
		return nil, fmt.Errorf("invalid vmid %d", vmid)
	}
	node, err := b.client.Node(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	return node.VirtualMachine(ctx, vmid)
}
