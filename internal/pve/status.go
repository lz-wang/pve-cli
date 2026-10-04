package pve

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/lz-wang/pvectl/internal/output"
)

// StatusService aggregates a HomeLab health snapshot. Every section tolerates
// partial failures: query problems become StatusIssue entries instead of
// aborting the whole report.
type StatusService struct {
	backend Backend
}

func NewStatusService(backend Backend) *StatusService {
	return &StatusService{backend: backend}
}

func (s *StatusService) Report(ctx context.Context) (output.StatusReport, error) {
	report := output.StatusReport{}
	var issues []output.StatusIssue

	nodes, err := s.backend.Nodes(ctx)
	if err != nil {
		return output.StatusReport{}, fmt.Errorf("list nodes: %w", err)
	}

	nodeRows := sortNodeRows(nodes)
	report.Nodes = summarizeNodes(nodeRows)
	report.Nodes.Rows = nodeRows

	guests, guestIssues := s.collectGuests(ctx, nodeNames(nodeRows))
	report.Guests = summarizeGuests(guests)
	issues = append(issues, guestIssues...)

	storages, storageIssues := s.collectStorages(ctx, nodeNames(nodeRows))
	report.Storages = summarizeStorages(storages)
	report.Storages.Rows = sortStorageRows(storages)
	issues = append(issues, storageIssues...)

	backups, backupIssues := s.collectBackups(ctx, storages)
	report.Backups = summarizeBackups(backups)
	issues = append(issues, backupIssues...)

	report.Issues = issues
	return report, nil
}

func (s *StatusService) collectGuests(ctx context.Context, nodes []string) ([]output.GuestRow, []output.StatusIssue) {
	var rows []output.GuestRow
	var issues []output.StatusIssue
	for _, node := range nodes {
		vmRows, err := s.backend.VMs(ctx, node)
		if err != nil {
			issues = append(issues, output.StatusIssue{Component: "guest", Message: fmt.Sprintf("list vm on node %s: %v", node, err)})
		} else {
			rows = append(rows, vmRows...)
		}

		lxcRows, err := s.backend.LXCs(ctx, node)
		if err != nil {
			issues = append(issues, output.StatusIssue{Component: "guest", Message: fmt.Sprintf("list lxc on node %s: %v", node, err)})
		} else {
			rows = append(rows, lxcRows...)
		}
	}
	return rows, issues
}

func (s *StatusService) collectStorages(ctx context.Context, nodes []string) ([]output.StorageRow, []output.StatusIssue) {
	var rows []output.StorageRow
	var issues []output.StatusIssue
	for _, node := range nodes {
		nodeRows, err := s.backend.Storages(ctx, node)
		if err != nil {
			issues = append(issues, output.StatusIssue{Component: "storage", Message: fmt.Sprintf("list storage on node %s: %v", node, err)})
			continue
		}
		rows = append(rows, nodeRows...)
	}
	return rows, issues
}

// collectBackups counts backup content across every backup-capable storage.
// Sources that cannot be queried on any node produce an issue instead of
// failing the report.
func (s *StatusService) collectBackups(ctx context.Context, storages []output.StorageRow) ([]output.BackupRow, []output.StatusIssue) {
	var rows []output.BackupRow
	var issues []output.StatusIssue
	for _, group := range backupSources(storages) {
		backupRows, err := listSourceBackups(ctx, s.backend, group)
		if err != nil {
			issues = append(issues, output.StatusIssue{
				Component: "backup",
				Message:   fmt.Sprintf("list backups on %s/%s: %v", group[0].Node, group[0].Storage, err),
			})
			continue
		}
		rows = append(rows, backupRows...)
	}
	return rows, issues
}

// backupSources returns the storages whose backups should be listed, one
// group per independent source. Shared storages expose identical content
// from every node, so each shared storage name forms a single source whose
// candidates cover every reporting node; local storages stay per node.
// Candidates able to answer (enabled and active) come first so reads skip
// degraded nodes.
func backupSources(storages []output.StorageRow) [][]output.StorageRow {
	order := make([]string, 0, len(storages))
	groups := make(map[string][]output.StorageRow, len(storages))
	for _, storage := range storages {
		if !storageHasContent(storage.Content, "backup") {
			continue
		}
		key := storage.Node + "/" + storage.Storage
		if storage.Shared {
			key = storage.Storage
		}
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], storage)
	}

	sources := make([][]output.StorageRow, 0, len(order))
	for _, key := range order {
		sources = append(sources, sortBackupCandidates(groups[key]))
	}
	return sources
}

// sortBackupCandidates partitions a source's node rows so readable nodes
// (enabled and active) are queried first, keeping the original order inside
// each partition.
func sortBackupCandidates(rows []output.StorageRow) []output.StorageRow {
	readable := make([]output.StorageRow, 0, len(rows))
	degraded := make([]output.StorageRow, 0, len(rows))
	for _, row := range rows {
		if row.Enabled && row.Active {
			readable = append(readable, row)
		} else {
			degraded = append(degraded, row)
		}
	}
	return append(readable, degraded...)
}

// listSourceBackups lists backups for one source group, trying candidates in
// preference order until a node answers. Shared storages expose identical
// content on every node, so the first successful read is authoritative; when
// every candidate fails, the joined per-node errors preserve the details.
func listSourceBackups(ctx context.Context, reader BackupReader, group []output.StorageRow) ([]output.BackupRow, error) {
	var lastErr error
	var errs []string
	for _, storage := range group {
		rows, err := reader.Backups(ctx, storage.Node, storage.Storage)
		if err == nil {
			return rows, nil
		}
		lastErr = err
		errs = append(errs, fmt.Sprintf("%s: %v", storage.Node, err))
	}
	if len(errs) <= 1 {
		return nil, lastErr
	}
	return nil, errors.New(strings.Join(errs, "; "))
}

func summarizeNodes(rows []output.NodeRow) output.NodeSummary {
	summary := output.NodeSummary{Total: len(rows)}
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.Status), "online") {
			summary.Online++
		} else {
			summary.Offline++
		}
	}
	return summary
}

func summarizeGuests(rows []output.GuestRow) output.GuestSummary {
	summary := output.GuestSummary{Total: len(rows)}
	for _, row := range rows {
		switch strings.ToLower(strings.TrimSpace(row.Status)) {
		case "running":
			summary.Running++
		case "stopped":
			summary.Stopped++
		}
		switch row.Kind {
		case "vm":
			summary.VM++
		case "lxc":
			summary.LXC++
		}
	}
	return summary
}

func summarizeStorages(rows []output.StorageRow) output.StorageSummary {
	summary := output.StorageSummary{Total: len(rows)}
	for _, row := range rows {
		if row.Active {
			summary.Active++
		}
	}
	return summary
}

func summarizeBackups(rows []output.BackupRow) output.BackupSummary {
	summary := output.BackupSummary{Count: len(rows)}
	for _, row := range rows {
		if row.CTime > summary.LatestCtime {
			summary.LatestCtime = row.CTime
		}
	}
	return summary
}

func nodeNames(rows []output.NodeRow) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Name == "" {
			continue
		}
		names = append(names, row.Name)
	}
	return names
}

func sortNodeRows(rows []output.NodeRow) []output.NodeRow {
	out := make([]output.NodeRow, len(rows))
	copy(out, rows)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
