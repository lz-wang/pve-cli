package pve

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

// Default storage usage thresholds in percent.
const (
	DefaultStorageWarnPercent = 85
	DefaultStorageFailPercent = 95
)

// CheckService inspects HomeLab health. doctor checks whether pve itself
// can work; check reports whether the HomeLab is healthy right now.
type CheckService struct {
	backend Backend
}

func NewCheckService(backend Backend) *CheckService {
	return &CheckService{backend: backend}
}

// CheckOptions configures thresholds and the optional backup SLA check.
// The backup coverage check only runs when both BackupTag and BackupMaxAge
// are set; it never assumes every guest must be backed up.
type CheckOptions struct {
	Node         string
	StorageWarn  int
	StorageFail  int
	BackupTag    string
	BackupMaxAge time.Duration
}

// CheckResult carries the check rows plus the overall exit semantics.
type CheckResult struct {
	Rows   []output.CheckRow
	Failed bool
	Warned bool
}

func (s *CheckService) Run(ctx context.Context, options CheckOptions) (CheckResult, error) {
	result := CheckResult{}
	if options.StorageWarn <= 0 {
		options.StorageWarn = DefaultStorageWarnPercent
	}
	if options.StorageFail <= 0 {
		options.StorageFail = DefaultStorageFailPercent
	}

	nodes, err := s.backend.Nodes(ctx)
	if err != nil {
		return CheckResult{}, fmt.Errorf("list nodes: %w", err)
	}

	nodeNames := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node.Name == "" {
			continue
		}
		if options.Node != "" && node.Name != options.Node {
			continue
		}
		nodeNames = append(nodeNames, node.Name)
		s.checkNode(node, &result)
	}

	// A filtered-out-everything run must never look healthy: cron and other
	// schedulers treat exit 0 as green, so an unknown --node is a failure.
	if options.Node != "" && len(nodeNames) == 0 {
		result.add(output.CheckRow{
			Check:    "node-exists",
			Status:   output.DoctorStatusFail,
			Resource: options.Node,
			Message:  "node not found",
		})
		return result, nil
	}

	storages, storageErrs := s.collectStorages(ctx, nodeNames)
	for _, nodeName := range sortedKeys(storageErrs) {
		result.add(output.CheckRow{
			Check:    "storage",
			Status:   output.DoctorStatusWarn,
			Resource: nodeName,
			Message:  fmt.Sprintf("list storage: %v", storageErrs[nodeName]),
		})
	}
	for _, storage := range storages {
		s.checkStorage(storage, options, &result)
	}

	if options.BackupTag != "" && options.BackupMaxAge > 0 {
		s.checkBackupCoverage(ctx, nodeNames, storages, options, &result)
	}

	return result, nil
}

func (s *CheckService) checkNode(node output.NodeRow, result *CheckResult) {
	status := strings.ToLower(strings.TrimSpace(node.Status))
	if status == "online" {
		result.add(output.CheckRow{
			Check:    "node-online",
			Status:   output.DoctorStatusOK,
			Resource: node.Name,
			Message:  "online",
		})
		return
	}
	message := node.Status
	if message == "" {
		message = "unknown status"
	}
	result.add(output.CheckRow{
		Check:    "node-online",
		Status:   output.DoctorStatusFail,
		Resource: node.Name,
		Message:  message,
	})
}

func (s *CheckService) collectStorages(ctx context.Context, nodes []string) ([]output.StorageRow, map[string]error) {
	var rows []output.StorageRow
	errs := make(map[string]error)
	for _, node := range nodes {
		nodeRows, err := s.backend.Storages(ctx, node)
		if err != nil {
			errs[node] = err
			continue
		}
		rows = append(rows, nodeRows...)
	}
	return rows, errs
}

func (s *CheckService) checkStorage(storage output.StorageRow, options CheckOptions, result *CheckResult) {
	resource := storage.Node + "/" + storage.Storage
	switch {
	case !storage.Enabled:
		result.add(output.CheckRow{
			Check:    "storage",
			Status:   output.DoctorStatusWarn,
			Resource: resource,
			Message:  "disabled",
		})
		return
	case !storage.Active:
		result.add(output.CheckRow{
			Check:    "storage",
			Status:   output.DoctorStatusFail,
			Resource: resource,
			Message:  "inactive",
		})
		return
	default:
		result.add(output.CheckRow{
			Check:    "storage",
			Status:   output.DoctorStatusOK,
			Resource: resource,
			Message:  "active",
		})
	}

	usage := storage.UsedFraction * 100
	switch {
	case usage >= float64(options.StorageFail):
		result.add(output.CheckRow{
			Check:    "storage-usage",
			Status:   output.DoctorStatusFail,
			Resource: resource,
			Message:  fmt.Sprintf("usage %.0f%% >= fail threshold %d%%", usage, options.StorageFail),
		})
	case usage >= float64(options.StorageWarn):
		result.add(output.CheckRow{
			Check:    "storage-usage",
			Status:   output.DoctorStatusWarn,
			Resource: resource,
			Message:  fmt.Sprintf("usage %.0f%% >= warn threshold %d%%", usage, options.StorageWarn),
		})
	}
}

// checkBackupCoverage verifies that guests tagged with BackupTag have a
// recent backup. Missing or stale coverage is a warning, not a failure.
func (s *CheckService) checkBackupCoverage(ctx context.Context, nodes []string, storages []output.StorageRow, options CheckOptions, result *CheckResult) {
	guests, guestErrs := s.collectTaggedGuests(ctx, nodes, options.BackupTag)
	for _, nodeName := range sortedKeys(guestErrs) {
		result.add(output.CheckRow{
			Check:    "backup-coverage",
			Status:   output.DoctorStatusWarn,
			Resource: nodeName,
			Message:  fmt.Sprintf("list guests: %v", guestErrs[nodeName]),
		})
	}
	if len(guests) == 0 {
		result.add(output.CheckRow{
			Check:   "backup-coverage",
			Status:  output.DoctorStatusSkip,
			Message: fmt.Sprintf("no guests tagged %q", options.BackupTag),
		})
		return
	}

	latest, backupErrs := s.collectLatestBackups(ctx, storages)
	for _, source := range sortedKeys(backupErrs) {
		result.add(output.CheckRow{
			Check:    "backup-coverage",
			Status:   output.DoctorStatusWarn,
			Resource: source,
			Message:  fmt.Sprintf("list backups: %v", backupErrs[source]),
		})
	}
	// A failing source can always hide a newer backup, so without a proven
	// recent backup no guest may be judged "no backup found" or "stale"
	// while any source is unqueryable; keep query outages and coverage gaps
	// apart for cron and agent consumers.
	hasUncertainSource := len(backupErrs) > 0
	now := uint64(time.Now().Unix())
	cutoff := now - uint64(options.BackupMaxAge.Seconds())
	for _, guest := range guests {
		resource := fmt.Sprintf("%s %d", guest.Kind, guest.VMID)
		ctime, ok := latest[backupGuestKey(guest.Kind, guest.VMID)]
		switch {
		case ok && ctime >= cutoff:
			// A compliant backup was proven to exist; a failing source
			// cannot overturn that verdict.
			result.add(output.CheckRow{
				Check:    "backup-coverage",
				Status:   output.DoctorStatusOK,
				Resource: resource,
				Message:  fmt.Sprintf("latest backup is %s old", output.FormatUptime(now-ctime)),
			})
		case hasUncertainSource:
			result.add(output.CheckRow{
				Check:    "backup-coverage",
				Status:   output.DoctorStatusWarn,
				Resource: resource,
				Message:  "backup status unavailable",
			})
		case !ok:
			result.add(output.CheckRow{
				Check:    "backup-coverage",
				Status:   output.DoctorStatusWarn,
				Resource: resource,
				Message:  "no backup found",
			})
		default:
			result.add(output.CheckRow{
				Check:    "backup-coverage",
				Status:   output.DoctorStatusWarn,
				Resource: resource,
				Message:  fmt.Sprintf("latest backup is %s old", output.FormatUptime(now-ctime)),
			})
		}
	}
}

func (s *CheckService) collectTaggedGuests(ctx context.Context, nodes []string, tag string) ([]output.GuestRow, map[string]error) {
	var rows []output.GuestRow
	errs := make(map[string]error)
	for _, node := range nodes {
		vmRows, err := s.backend.VMs(ctx, node)
		if err != nil {
			errs[node] = err
		} else {
			rows = append(rows, vmRows...)
		}
		lxcRows, err := s.backend.LXCs(ctx, node)
		if err != nil {
			errs[node] = err
		} else {
			rows = append(rows, lxcRows...)
		}
	}

	tagged := make([]output.GuestRow, 0, len(rows))
	for _, row := range rows {
		if MatchGuestTags(ParseGuestTags(row.Tags), []string{tag}, TagMatchAll) {
			tagged = append(tagged, row)
		}
	}
	sortGuestRows(tagged)
	return tagged, errs
}

// collectLatestBackups returns the newest backup per guest plus one error
// per backup source that could not be queried on any of its candidate nodes.
// Shared storages are queried once, on the first node that can answer,
// matching the status backup summary.
func (s *CheckService) collectLatestBackups(ctx context.Context, storages []output.StorageRow) (map[string]uint64, map[string]error) {
	latest := make(map[string]uint64)
	errs := make(map[string]error)
	for _, group := range backupSources(storages) {
		rows, err := listSourceBackups(ctx, s.backend, group)
		if err != nil {
			errs[group[0].Node+"/"+group[0].Storage] = err
			continue
		}
		for _, row := range rows {
			key := backupGuestKey(row.Kind, row.VMID)
			if row.CTime > latest[key] {
				latest[key] = row.CTime
			}
		}
	}
	return latest, errs
}

func backupGuestKey(kind string, vmid uint64) string {
	return kind + "/" + fmt.Sprint(vmid)
}

func (r *CheckResult) add(row output.CheckRow) {
	r.Rows = append(r.Rows, row)
	switch row.Status {
	case output.DoctorStatusFail:
		r.Failed = true
	case output.DoctorStatusWarn:
		r.Warned = true
	}
}

func sortedKeys(m map[string]error) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
