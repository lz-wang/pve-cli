package pve

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

// Firewall scope kinds.
const (
	FirewallScopeNode = "node"
	FirewallScopeVM   = "vm"
	FirewallScopeLXC  = "lxc"
)

// FirewallBackend covers read-only firewall inventory for nodes, VMs, and
// LXC containers. Guest queries are included so vm/lxc scopes can locate a
// guest across the cluster when --node is omitted. Rule mutation is out of
// scope.
type FirewallBackend interface {
	GuestBackend
	FirewallStatus(ctx context.Context, scope FirewallScope) (output.FirewallStatusRow, error)
	FirewallRules(ctx context.Context, scope FirewallScope) ([]output.FirewallRuleRow, error)
}

// FirewallScope identifies which firewall tree to inspect.
type FirewallScope struct {
	Node string
	Type string // node, vm, or lxc
	VMID int
}

// ParseFirewallScope validates flags into a scope. The node is required for
// the node scope; vm/lxc scopes may leave it empty so the guest is located
// across the cluster instead.
func ParseFirewallScope(node, kind string, vmid int) (FirewallScope, error) {
	node = strings.TrimSpace(node)
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case FirewallScopeNode, "":
		if node == "" {
			return FirewallScope{}, fmt.Errorf("node is required for --scope node")
		}
		return FirewallScope{Node: node, Type: FirewallScopeNode}, nil
	case FirewallScopeVM, FirewallScopeLXC:
		if vmid <= 0 {
			return FirewallScope{}, fmt.Errorf("--vmid is required for --scope %s", kind)
		}
		return FirewallScope{Node: node, Type: strings.ToLower(strings.TrimSpace(kind)), VMID: vmid}, nil
	default:
		return FirewallScope{}, fmt.Errorf("invalid firewall scope %q, expected node, vm, or lxc", kind)
	}
}

// ParseFirewallScopeKind validates the --scope value alone.
func ParseFirewallScopeKind(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", FirewallScopeNode:
		return FirewallScopeNode, nil
	case FirewallScopeVM, FirewallScopeLXC:
		return strings.ToLower(strings.TrimSpace(value)), nil
	default:
		return "", fmt.Errorf("invalid firewall scope %q, expected node, vm, or lxc", value)
	}
}

// FirewallService exposes read-only firewall inventory with the same node
// semantics as every other VMID-oriented command: --node is optional for
// vm/lxc scopes, and an omitted node is located by traversing the cluster.
type FirewallService struct {
	backend FirewallBackend
	logger  *slog.Logger
	verbose bool
}

func NewFirewallService(backend FirewallBackend, logger *slog.Logger, verbose bool) *FirewallService {
	return &FirewallService{backend: backend, logger: logger, verbose: verbose}
}

// Status reports whether the firewall is enabled for the scope.
func (s *FirewallService) Status(ctx context.Context, scope FirewallScope) (output.FirewallStatusRow, error) {
	scope, err := s.resolveScope(ctx, scope)
	if err != nil {
		return output.FirewallStatusRow{}, err
	}
	return s.backend.FirewallStatus(ctx, scope)
}

// Rules lists firewall rules for the scope in positional order.
func (s *FirewallService) Rules(ctx context.Context, scope FirewallScope) ([]output.FirewallRuleRow, error) {
	scope, err := s.resolveScope(ctx, scope)
	if err != nil {
		return nil, err
	}
	return s.backend.FirewallRules(ctx, scope)
}

// resolveScope locates the guest across the cluster when a vm/lxc scope is
// used without --node, matching `vm get` and the agent commands.
func (s *FirewallService) resolveScope(ctx context.Context, scope FirewallScope) (FirewallScope, error) {
	if scope.Node != "" || scope.Type == FirewallScopeNode {
		return scope, nil
	}
	svc := NewVMService(s.backend, TaskRunner{}, s.logger, s.verbose)
	if scope.Type == FirewallScopeLXC {
		svc = NewLXCService(s.backend, TaskRunner{}, s.logger, s.verbose)
	}
	row, err := svc.Get(ctx, scope.VMID, "")
	if err != nil {
		return FirewallScope{}, err
	}
	scope.Node = row.Node
	return scope, nil
}

// FirewallStatus reports whether the firewall is enabled for the scope.
func (b *ProxmoxBackend) FirewallStatus(ctx context.Context, scope FirewallScope) (output.FirewallStatusRow, error) {
	row := output.FirewallStatusRow{Scope: scope.Type, Node: scope.Node, VMID: uint64(scope.VMID)}
	switch scope.Type {
	case FirewallScopeNode:
		node, err := b.client.Node(ctx, scope.Node)
		if err != nil {
			return row, err
		}
		options, err := node.FirewallOptionGet(ctx)
		if err != nil {
			return row, err
		}
		row.Enabled = options.Enable != nil && bool(*options.Enable)
		return row, nil
	case FirewallScopeVM:
		node, err := b.client.Node(ctx, scope.Node)
		if err != nil {
			return row, err
		}
		vm, err := node.VirtualMachine(ctx, scope.VMID)
		if err != nil {
			return row, err
		}
		firewall, err := vm.Firewall(ctx)
		if err != nil {
			return row, err
		}
		row.Enabled = firewall.Options != nil && firewall.Options.Enable != nil && bool(*firewall.Options.Enable)
		return row, nil
	case FirewallScopeLXC:
		node, err := b.client.Node(ctx, scope.Node)
		if err != nil {
			return row, err
		}
		ct, err := node.Container(ctx, scope.VMID)
		if err != nil {
			return row, err
		}
		firewall, err := ct.Firewall(ctx)
		if err != nil {
			return row, err
		}
		row.Enabled = firewall.Options != nil && firewall.Options.Enable != nil && bool(*firewall.Options.Enable)
		return row, nil
	default:
		return row, fmt.Errorf("invalid firewall scope %q", scope.Type)
	}
}

// FirewallRules lists rules for the scope in positional order.
func (b *ProxmoxBackend) FirewallRules(ctx context.Context, scope FirewallScope) ([]output.FirewallRuleRow, error) {
	switch scope.Type {
	case FirewallScopeNode:
		node, err := b.client.Node(ctx, scope.Node)
		if err != nil {
			return nil, err
		}
		rules, err := node.FirewallRules(ctx)
		if err != nil {
			return nil, err
		}
		rows := make([]output.FirewallRuleRow, 0, len(rules))
		for _, rule := range rules {
			rows = append(rows, firewallRuleRow(scope, rule))
		}
		return rows, nil
	case FirewallScopeVM:
		node, err := b.client.Node(ctx, scope.Node)
		if err != nil {
			return nil, err
		}
		vm, err := node.VirtualMachine(ctx, scope.VMID)
		if err != nil {
			return nil, err
		}
		rules, err := vm.FirewallRules(ctx)
		if err != nil {
			return nil, err
		}
		rows := make([]output.FirewallRuleRow, 0, len(rules))
		for _, rule := range rules {
			rows = append(rows, firewallRuleRow(scope, rule))
		}
		return rows, nil
	case FirewallScopeLXC:
		node, err := b.client.Node(ctx, scope.Node)
		if err != nil {
			return nil, err
		}
		ct, err := node.Container(ctx, scope.VMID)
		if err != nil {
			return nil, err
		}
		rules, err := ct.FirewallRules(ctx)
		if err != nil {
			return nil, err
		}
		rows := make([]output.FirewallRuleRow, 0, len(rules))
		for _, rule := range rules {
			rows = append(rows, firewallRuleRow(scope, rule))
		}
		return rows, nil
	default:
		return nil, fmt.Errorf("invalid firewall scope %q", scope.Type)
	}
}

func firewallRuleRow(scope FirewallScope, rule *proxmox.FirewallRule) output.FirewallRuleRow {
	if rule == nil {
		return output.FirewallRuleRow{Scope: scope.Type, Node: scope.Node, VMID: uint64(scope.VMID)}
	}
	return output.FirewallRuleRow{
		Scope:       scope.Type,
		Node:        scope.Node,
		VMID:        uint64(scope.VMID),
		Position:    rule.Pos,
		Enabled:     rule.Enable != 0,
		Direction:   rule.Type,
		Action:      rule.Action,
		Interface:   rule.Iface,
		Source:      rule.Source,
		Destination: rule.Dest,
		Protocol:    rule.Proto,
		SourcePort:  rule.Sport,
		DestPort:    rule.Dport,
		Log:         rule.Log,
		Comment:     rule.Comment,
	}
}
