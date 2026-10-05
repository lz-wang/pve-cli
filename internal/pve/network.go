package pve

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

// NetworkBackend covers read-only node network inventory. Remote network
// mutation has too large a blast radius for a HomeLab CLI, so this backend
// intentionally has no create/update/delete/apply surface.
type NetworkBackend interface {
	NodeBackend
	Networks(ctx context.Context, node string, options NetworkListOptions) ([]output.NetworkRow, error)
	Network(ctx context.Context, node, iface string) (output.NetworkRow, error)
}

// NetworkListOptions filters the interface listing.
type NetworkListOptions struct {
	Type   string
	Active bool
}

// NetworkService implements read-only network inventory.
type NetworkService struct {
	backend NetworkBackend
	logger  *slog.Logger
	verbose bool
}

func NewNetworkService(backend NetworkBackend, logger *slog.Logger, verbose bool) *NetworkService {
	return &NetworkService{backend: backend, logger: logger, verbose: verbose}
}

// List returns interface rows for one node, or aggregated across nodes with
// the usual partial-success behavior.
func (s *NetworkService) List(ctx context.Context, node string, options NetworkListOptions) ([]output.NetworkRow, error) {
	if strings.TrimSpace(node) != "" {
		rows, err := s.backend.Networks(ctx, node, options)
		if err != nil {
			return nil, err
		}
		return sortNetworkRows(filterNetworkRows(rows, options)), nil
	}

	nodes, err := s.backend.Nodes(ctx)
	if err != nil {
		return nil, err
	}

	var rows []output.NetworkRow
	successes := 0
	var firstErr error
	for _, nodeRow := range nodes {
		if nodeRow.Name == "" {
			continue
		}
		nodeRows, err := s.backend.Networks(ctx, nodeRow.Name, options)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			s.debug("skip node", "node", nodeRow.Name, "error", err)
			continue
		}
		successes++
		rows = append(rows, nodeRows...)
	}
	if successes == 0 && firstErr != nil {
		return nil, fmt.Errorf("list networks: no nodes could be queried: %w", firstErr)
	}
	return sortNetworkRows(filterNetworkRows(rows, options)), nil
}

// Get returns a single interface row.
func (s *NetworkService) Get(ctx context.Context, node, iface string) (output.NetworkRow, error) {
	iface = strings.TrimSpace(iface)
	if iface == "" {
		return output.NetworkRow{}, fmt.Errorf("interface name is required")
	}
	return s.backend.Network(ctx, node, iface)
}

func filterNetworkRows(rows []output.NetworkRow, options NetworkListOptions) []output.NetworkRow {
	networkType := strings.ToLower(strings.TrimSpace(options.Type))
	if networkType == "" && !options.Active {
		return rows
	}

	out := rows[:0]
	for _, row := range rows {
		if networkType != "" && strings.ToLower(strings.TrimSpace(row.Type)) != networkType {
			continue
		}
		if options.Active && !row.Active {
			continue
		}
		out = append(out, row)
	}
	return out
}

func sortNetworkRows(rows []output.NetworkRow) []output.NetworkRow {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Node != rows[j].Node {
			return rows[i].Node < rows[j].Node
		}
		return rows[i].Name < rows[j].Name
	})
	return rows
}

func (b *ProxmoxBackend) Networks(ctx context.Context, nodeName string, options NetworkListOptions) ([]output.NetworkRow, error) {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return nil, fmt.Errorf("node is required")
	}
	node, err := b.client.Node(ctx, nodeName)
	if err != nil {
		return nil, err
	}

	var networks proxmox.NodeNetworks
	if strings.TrimSpace(options.Type) != "" {
		networks, err = node.Networks(ctx, options.Type)
	} else {
		networks, err = node.Networks(ctx)
	}
	if err != nil {
		return nil, err
	}

	rows := make([]output.NetworkRow, 0, len(networks))
	for _, network := range networks {
		rows = append(rows, networkRow(nodeName, network))
	}
	return rows, nil
}

func (b *ProxmoxBackend) Network(ctx context.Context, nodeName, iface string) (output.NetworkRow, error) {
	nodeName = strings.TrimSpace(nodeName)
	iface = strings.TrimSpace(iface)
	if nodeName == "" {
		return output.NetworkRow{}, fmt.Errorf("node is required")
	}
	if iface == "" {
		return output.NetworkRow{}, fmt.Errorf("interface name is required")
	}
	node, err := b.client.Node(ctx, nodeName)
	if err != nil {
		return output.NetworkRow{}, err
	}
	network, err := node.Network(ctx, iface)
	if err != nil {
		return output.NetworkRow{}, err
	}
	return networkRow(nodeName, network), nil
}

func networkRow(nodeName string, network *proxmox.NodeNetwork) output.NetworkRow {
	if network == nil {
		return output.NetworkRow{Node: nodeName}
	}
	return output.NetworkRow{
		Node:        nodeName,
		Name:        network.Iface,
		Type:        network.Type,
		Active:      network.Active != 0,
		Autostart:   network.Autostart != 0,
		Address:     network.Address,
		CIDR:        network.CIDR,
		Gateway:     network.Gateway,
		BridgePorts: network.BridgePorts,
		BondSlaves:  network.Slaves,
		VLANAware:   network.BridgeVLANAware != 0,
		Comments:    network.Comments,
	}
}

func (s *NetworkService) debug(msg string, args ...any) {
	if s.verbose && s.logger != nil {
		s.logger.Debug(msg, args...)
	}
}
