package pve

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

// PlanRestore performs the read-only restore preflight: target VMID
// availability, archive readability, target storage existence/activation
// and capacity, mapped bridge existence, and honest unknowns for facts
// inside the archive. The restored guest is never started automatically.
func (s *RestoreService) PlanRestore(ctx context.Context, options RestoreOptions) (output.MigrationPlan, error) {
	kind, err := parseRestoreKind(options.Kind)
	if err != nil {
		return output.MigrationPlan{}, err
	}
	if strings.TrimSpace(options.Archive) == "" {
		return output.MigrationPlan{}, fmt.Errorf("archive is required")
	}
	if strings.TrimSpace(options.Node) == "" {
		return output.MigrationPlan{}, fmt.Errorf("node is required")
	}
	if options.VMID <= 0 {
		return output.MigrationPlan{}, fmt.Errorf("%w: invalid vmid %d", ErrInvalid, options.VMID)
	}

	plan := output.MigrationPlan{
		Resource:   kind,
		VMID:       options.VMID,
		Node:       strings.TrimSpace(options.Node),
		TargetNode: strings.TrimSpace(options.Node),
		TargetVMID: options.VMID,
	}
	s.planVMIDFree(ctx, &plan, options)
	archiveBytes := s.planArchiveReadable(ctx, &plan, options)
	s.planTargetStorage(ctx, &plan, options, archiveBytes)
	s.planBridgeMap(ctx, &plan, options)
	plan.Checks = append(plan.Checks,
		output.MigrationCheckRow{
			Check:   "onboot",
			Status:  output.MigrationCheckUnknown,
			Message: "the archive's onboot setting is only visible after the restore; the guest is never started by pve-cli",
		},
		macIPCheck(),
		output.MigrationCheckRow{
			Check:   "mount",
			Status:  output.MigrationCheckUnknown,
			Message: "bind mounts and device passthrough inside the archive are only visible after the restore; re-check the restored configuration",
		},
		output.MigrationCheckRow{
			Check:   "start",
			Status:  output.MigrationCheckOK,
			Message: "the restored guest stays stopped; start it explicitly after validating",
		},
	)
	plan.Status = overallMigrationStatus(plan.Checks)
	return plan, nil
}

func (s *RestoreService) planVMIDFree(ctx context.Context, plan *output.MigrationPlan, options RestoreOptions) {
	if err := s.ensureVMIDFree(ctx, options.VMID); err != nil {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "vmid", Status: output.MigrationCheckFail, Message: err.Error(),
		})
		return
	}
	plan.Checks = append(plan.Checks, output.MigrationCheckRow{
		Check: "vmid", Status: output.MigrationCheckOK, Message: fmt.Sprintf("vmid %d is free on the cluster", options.VMID),
	})
}

// planArchiveReadable verifies the archive is listed on its storage and
// returns the archive size in bytes (0 when unknown) for capacity checks.
func (s *RestoreService) planArchiveReadable(ctx context.Context, plan *output.MigrationPlan, options RestoreOptions) uint64 {
	storage, subpath, err := parseBackupVolid(options.Archive)
	if err != nil {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "archive", Status: output.MigrationCheckUnknown,
			Message: "archive is not a storage volume id; PVE will resolve it (for example a local file path)",
		})
		return 0
	}
	reader, ok := s.backend.(BackupReader)
	if !ok {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "archive", Status: output.MigrationCheckUnknown, Message: "backup listing unavailable",
		})
		return 0
	}
	rows, err := reader.Backups(ctx, options.Node, storage)
	if err != nil {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "archive", Status: output.MigrationCheckFail, Message: fmt.Sprintf("storage %s: %v", storage, ClassifyError(err)),
		})
		return 0
	}
	for _, row := range rows {
		if row.VolID == strings.TrimSpace(options.Archive) {
			plan.Checks = append(plan.Checks, output.MigrationCheckRow{
				Check: "archive", Status: output.MigrationCheckOK,
				Message: fmt.Sprintf("%s readable (%s, %s)", subpath, row.Format, output.FormatBytes(row.Size)),
			})
			return row.Size
		}
	}
	plan.Checks = append(plan.Checks, output.MigrationCheckRow{
		Check: "archive", Status: output.MigrationCheckFail,
		Message: fmt.Sprintf("archive %s not found on storage %s node %s", subpath, storage, options.Node),
	})
	return 0
}

func (s *RestoreService) planTargetStorage(ctx context.Context, plan *output.MigrationPlan, options RestoreOptions, archiveBytes uint64) {
	storage := strings.TrimSpace(options.Storage)
	if storage == "" {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "storage", Status: output.MigrationCheckUnknown,
			Message: "no target storage given; PVE picks the default rootfs storage",
		})
		return
	}
	storageBackend, ok := s.backend.(StorageBackend)
	if !ok {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "storage", Status: output.MigrationCheckUnknown, Message: "storage inventory unavailable",
		})
		return
	}
	rows, err := storageBackend.Storages(ctx, options.Node)
	if err != nil {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "storage", Status: output.MigrationCheckFail, Message: fmt.Sprintf("storage inventory: %v", ClassifyError(err)),
		})
		return
	}
	for _, row := range rows {
		if row.Storage != storage {
			continue
		}
		if !row.Active || !row.Enabled {
			plan.Checks = append(plan.Checks, output.MigrationCheckRow{
				Check: "storage", Status: output.MigrationCheckFail, Message: fmt.Sprintf("storage %s is not active on node %s", storage, options.Node),
			})
			return
		}
		if archiveBytes > 0 && row.Avail < archiveBytes {
			plan.Checks = append(plan.Checks, output.MigrationCheckRow{
				Check: "storage", Status: output.MigrationCheckFail,
				Message: fmt.Sprintf("storage %s has %s available, archive needs ~%s", storage, output.FormatBytes(row.Avail), output.FormatBytes(archiveBytes)),
			})
			return
		}
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "storage", Status: output.MigrationCheckOK,
			Message: fmt.Sprintf("storage %s active on %s (%s available; the compressed archive size is only a lower bound for the restored footprint)", storage, options.Node, output.FormatBytes(row.Avail)),
		})
		return
	}
	plan.Checks = append(plan.Checks, output.MigrationCheckRow{
		Check: "storage", Status: output.MigrationCheckFail, Message: fmt.Sprintf("storage %s not found on node %s", storage, options.Node),
	})
}

// planBridgeMap verifies that every mapped target bridge exists on the
// target node. Source bridges live inside the archive and stay unknown.
func (s *RestoreService) planBridgeMap(ctx context.Context, plan *output.MigrationPlan, options RestoreOptions) {
	if len(options.BridgeMap) == 0 {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "network", Status: output.MigrationCheckUnknown,
			Message: "no bridge mapping given; the archive's bridges restore as-is — verify they exist on the target node",
		})
		return
	}
	networkBackend, ok := s.backend.(NetworkBackend)
	if !ok {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "network", Status: output.MigrationCheckUnknown, Message: "network inventory unavailable",
		})
		return
	}
	rows, err := networkBackend.Networks(ctx, options.Node, NetworkListOptions{})
	if err != nil {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "network", Status: output.MigrationCheckFail, Message: fmt.Sprintf("network inventory: %v", ClassifyError(err)),
		})
		return
	}
	existing := map[string]bool{}
	for _, row := range rows {
		existing[row.Name] = true
	}
	var failures []string
	for source, target := range options.BridgeMap {
		if !existing[target] {
			failures = append(failures, fmt.Sprintf("mapped bridge %s->%s: target missing on node %s", source, target, options.Node))
		}
	}
	if len(failures) > 0 {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "network", Status: output.MigrationCheckFail, Message: strings.Join(failures, "; "),
		})
		return
	}
	plan.Checks = append(plan.Checks, output.MigrationCheckRow{
		Check: "network", Status: output.MigrationCheckOK,
		Message: "all mapped target bridges exist; mappings apply after the restore completes",
	})
}

// applyBridgeMap rewrites the bridge= attribute of the restored guest's
// network devices according to the mapping. It runs only after a completed
// (--wait) restore task. Every explicit mapping must match at least one net
// device — a mapping matching nothing is a configuration error, never a
// silent no-op. The write submits under the configuration digest and is
// verified by reading the configuration back: only every mapped device
// reporting its target bridge counts as applied.
func (s *RestoreService) applyBridgeMap(ctx context.Context, options RestoreOptions) error {
	if len(options.BridgeMap) == 0 {
		return nil
	}
	var guest Guest
	var err error
	if options.Kind == BackupKindVM {
		guest, err = s.backend.VM(ctx, options.Node, options.VMID)
	} else {
		guest, err = s.backend.LXC(ctx, options.Node, options.VMID)
	}
	if err != nil {
		return fmt.Errorf("bridge mapping: resolve restored guest: %w", ClassifyError(err))
	}
	configBackend, ok := s.backend.(NativeConfigBackend)
	if !ok {
		return fmt.Errorf("%w: bridge mapping needs native config access", ErrUnavailable)
	}
	config, err := configBackend.GuestConfig(ctx, options.Kind, options.Node, options.VMID)
	if err != nil {
		return fmt.Errorf("bridge mapping: read config: %w", ClassifyError(err))
	}
	values := map[string]string{}
	targets := map[string]string{}
	matched := map[string]int{}
	for key, value := range config {
		text, ok := value.(string)
		if !ok || !strings.HasPrefix(key, "net") {
			continue
		}
		bridge := optionAttributeValue(text, "bridge")
		target, mapped := options.BridgeMap[bridge]
		if !mapped {
			continue
		}
		values[key] = mergeNetAttributes(text, map[string]string{"bridge": target})
		targets[key] = target
		matched[bridge]++
	}
	var unmatched []string
	for source := range options.BridgeMap {
		if matched[source] == 0 {
			unmatched = append(unmatched, source)
		}
	}
	if len(unmatched) > 0 {
		sort.Strings(unmatched)
		return fmt.Errorf("%w: bridge mapping matched no network device on the restored guest: %s (inspect the restored configuration with `pve guest config`)", ErrInvalid, strings.Join(unmatched, ", "))
	}

	// Submit under the digest read with the configuration: a concurrent
	// write between this read and the submit is rejected by PVE instead of
	// silently overwritten.
	digest, _ := config["digest"].(string)
	var task Task
	if submitter, ok := guest.(DigestConfigSubmitter); ok && strings.TrimSpace(digest) != "" {
		task, err = submitter.ConfigWithDigest(ctx, values, digest)
	} else {
		task, err = guest.Config(ctx, values)
	}
	if err != nil {
		return fmt.Errorf("bridge mapping: update config: %w", ClassifyError(err))
	}
	// The mapping is part of the completed restore: its config write is
	// always awaited, independently of the user's --wait choice, because
	// the read-back verification below needs the settled state.
	if task != nil {
		runner := s.tasks
		runner.Wait = true
		if runner.WaitTimeout <= 0 {
			runner.WaitTimeout = 2 * time.Minute
		}
		operation, waitErr := runner.Execute(ctx, task)
		if waitErr != nil || operation.Status != "succeeded" {
			detail := detailOrError(operation.Error, waitErr)
			return fmt.Errorf("bridge mapping: config task did not complete (%s): %s", operation.Status, detail)
		}
	}

	// An accepted write is not an applied write: read the configuration
	// back and require every mapped device to report its target bridge.
	verifyConfig, err := configBackend.GuestConfig(ctx, options.Kind, options.Node, options.VMID)
	if err != nil {
		return fmt.Errorf("bridge mapping: read back config: %w", ClassifyError(err))
	}
	for key, target := range targets {
		applied, ok := verifyConfig[key].(string)
		if !ok {
			return fmt.Errorf("%w: bridge mapping did not take effect: device %s is missing from the configuration after the write", ErrConflict, key)
		}
		if actual := optionAttributeValue(applied, "bridge"); actual != target {
			return fmt.Errorf("%w: bridge mapping did not take effect: device %s reports bridge %q, expected %q", ErrConflict, key, actual, target)
		}
	}
	return nil
}

// optionAttributeValue returns one attribute value of a PVE option string.
func optionAttributeValue(value, attribute string) string {
	for part := range strings.SplitSeq(value, ",") {
		name, attr, found := strings.Cut(strings.TrimSpace(part), "=")
		if found && strings.TrimSpace(name) == attribute {
			return strings.TrimSpace(attr)
		}
	}
	return ""
}
