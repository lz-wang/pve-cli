package pve

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

// RemoteMigrationCheckOptions configures the post-migration validation.
type RemoteMigrationCheckOptions struct {
	// SourceVMID identifies the source guest on the source cluster; 0 means
	// source and target share the ID.
	SourceVMID int
	Target     GuestBackend
	TargetNode string
}

// CheckRemoteMigration validates the state after a cross-profile
// migration. It is read-only and never rolls anything back: failures keep
// both sides intact so an operator can repair and re-run.
func (s *GuestService) CheckRemoteMigration(ctx context.Context, targetVMID int, options RemoteMigrationCheckOptions) (output.MigrationPlan, error) {
	if s.kind != "lxc" {
		return output.MigrationPlan{}, fmt.Errorf("%w: post-migration validation is only supported for lxc guests", ErrInvalid)
	}
	if targetVMID <= 0 {
		return output.MigrationPlan{}, fmt.Errorf("%w: invalid target vmid %d", ErrInvalid, targetVMID)
	}
	if options.Target == nil {
		return output.MigrationPlan{}, fmt.Errorf("%w: target backend is required", ErrInvalid)
	}
	if options.SourceVMID == 0 {
		options.SourceVMID = targetVMID
	}

	plan := output.MigrationPlan{
		Resource:   s.kind,
		VMID:       targetVMID,
		TargetNode: options.TargetNode,
		TargetVMID: targetVMID,
	}

	sourceRow, sourcePresent := s.checkMigrationSource(ctx, &plan, options)
	targetRow := s.checkMigrationTarget(ctx, &plan, options, targetVMID)
	sourceConfig := s.readSourceConfig(ctx, sourceRow.Node, options.SourceVMID, sourcePresent)
	targetConfig := s.readTargetConfig(ctx, options, targetRow, targetVMID)
	s.checkTargetConfig(ctx, &plan, options, targetConfig)
	s.checkTargetStartable(&plan, targetRow, targetConfig)
	s.checkPostMigrationMAC(ctx, &plan, sourcePresent, sourceRow, sourceConfig, targetConfig)
	s.checkMounts(&plan, targetConfig)

	plan.Node = sourceRow.Node
	plan.Status = overallMigrationStatus(plan.Checks)
	return plan, nil
}

// checkMigrationSource records whether the source guest still exists and
// whether it is still running.
func (s *GuestService) checkMigrationSource(ctx context.Context, plan *output.MigrationPlan, options RemoteMigrationCheckOptions) (output.GuestRow, bool) {
	guest, err := s.resolve(ctx, options.SourceVMID, "")
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			plan.Checks = append(plan.Checks, output.MigrationCheckRow{
				Check: "source", Status: output.MigrationCheckOK,
				Message: fmt.Sprintf("source guest %d no longer exists on the source cluster", options.SourceVMID),
			})
			return output.GuestRow{}, false
		}
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "source", Status: output.MigrationCheckUnknown, Message: fmt.Sprintf("source cluster: %v", err),
		})
		return output.GuestRow{}, false
	}
	row := guest.Row()
	switch row.Status {
	case "running":
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "source", Status: output.MigrationCheckWarning,
			Message: fmt.Sprintf("source guest %d is still running on %s (kept by default) — starting the target as well would duplicate services", options.SourceVMID, row.Node),
		})
	default:
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "source", Status: output.MigrationCheckWarning,
			Message: fmt.Sprintf("source guest %d retained on %s in state %q; remove it manually after validating the target", options.SourceVMID, row.Node, row.Status),
		})
	}
	return row, true
}

func (s *GuestService) checkMigrationTarget(ctx context.Context, plan *output.MigrationPlan, options RemoteMigrationCheckOptions, targetVMID int) output.GuestRow {
	nodes, err := options.Target.Nodes(ctx)
	if err != nil {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "target", Status: output.MigrationCheckFail, Message: fmt.Sprintf("target cluster unreachable: %v", ClassifyError(err)),
		})
		return output.GuestRow{}
	}
	for _, node := range nodes {
		if options.TargetNode != "" && node.Name != options.TargetNode {
			continue
		}
		guest, err := options.Target.LXC(ctx, node.Name, targetVMID)
		if err != nil {
			if errors.Is(ClassifyError(err), ErrNotFound) {
				continue
			}
			plan.Checks = append(plan.Checks, output.MigrationCheckRow{
				Check: "target", Status: output.MigrationCheckFail, Message: fmt.Sprintf("target node %s: %v", node.Name, ClassifyError(err)),
			})
			return output.GuestRow{}
		}
		row := guest.Row()
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "target", Status: output.MigrationCheckOK,
			Message: fmt.Sprintf("target guest %d present on node %s (state %q)", targetVMID, row.Node, row.Status),
		})
		return row
	}
	plan.Checks = append(plan.Checks, output.MigrationCheckRow{
		Check: "target", Status: output.MigrationCheckFail,
		Message: fmt.Sprintf("target guest %d not found%s", targetVMID, targetScopeHint(options.TargetNode)),
	})
	return output.GuestRow{}
}

func targetScopeHint(targetNode string) string {
	if targetNode == "" {
		return " on any target node"
	}
	return " on target node " + targetNode
}

func (s *GuestService) readSourceConfig(ctx context.Context, node string, vmid int, present bool) map[string]any {
	if !present {
		return nil
	}
	backend, ok := s.backend.(NativeConfigBackend)
	if !ok {
		return nil
	}
	config, err := backend.GuestConfig(ctx, s.kind, node, vmid)
	if err != nil {
		return nil
	}
	return config
}

func (s *GuestService) readTargetConfig(ctx context.Context, options RemoteMigrationCheckOptions, row output.GuestRow, targetVMID int) map[string]any {
	if row.Node == "" {
		return nil
	}
	backend, ok := options.Target.(NativeConfigBackend)
	if !ok {
		return nil
	}
	config, err := backend.GuestConfig(ctx, "lxc", row.Node, targetVMID)
	if err != nil {
		return nil
	}
	return config
}

// checkTargetConfig verifies the target storage and bridge wiring.
func (s *GuestService) checkTargetConfig(ctx context.Context, plan *output.MigrationPlan, options RemoteMigrationCheckOptions, config map[string]any) {
	if config == nil {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "target_config", Status: output.MigrationCheckUnknown, Message: "target configuration unreadable",
		})
		return
	}
	var failures []string

	if storageBackend, ok := options.Target.(StorageBackend); ok && options.TargetNode != "" {
		rows, err := storageBackend.Storages(ctx, options.TargetNode)
		if err == nil {
			available := map[string]bool{}
			active := map[string]bool{}
			for _, row := range rows {
				available[row.Storage] = true
				active[row.Storage] = row.Active && row.Enabled
			}
			volumes := sourceVolumes(config)
			names := make([]string, 0, len(volumes))
			for name := range volumes {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				storage := volumes[name].storage
				if !available[storage] {
					failures = append(failures, fmt.Sprintf("%s: storage %q missing on target node %s", name, storage, options.TargetNode))
					continue
				}
				if !active[storage] {
					failures = append(failures, fmt.Sprintf("%s: storage %q inactive on target", name, storage))
				}
			}
		}
	}

	if networkBackend, ok := options.Target.(NetworkBackend); ok && options.TargetNode != "" {
		rows, err := networkBackend.Networks(ctx, options.TargetNode, NetworkListOptions{})
		if err == nil {
			existing := map[string]bool{}
			for _, row := range rows {
				existing[row.Name] = true
			}
			for key, value := range config {
				text, ok := value.(string)
				if !ok || !strings.HasPrefix(key, "net") {
					continue
				}
				for part := range strings.SplitSeq(text, ",") {
					name, attr, found := strings.Cut(strings.TrimSpace(part), "=")
					if found && strings.TrimSpace(name) == "bridge" && !existing[strings.TrimSpace(attr)] {
						failures = append(failures, fmt.Sprintf("%s: bridge %q missing on target node %s", key, strings.TrimSpace(attr), options.TargetNode))
					}
				}
			}
		}
	}

	if len(failures) > 0 {
		sort.Strings(failures)
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "target_config", Status: output.MigrationCheckFail, Message: strings.Join(failures, "; "),
		})
		return
	}
	plan.Checks = append(plan.Checks, output.MigrationCheckRow{
		Check: "target_config", Status: output.MigrationCheckOK, Message: "target storage and bridge wiring verified",
	})
}

// checkTargetStartable reports whether the target may be started now.
func (s *GuestService) checkTargetStartable(plan *output.MigrationPlan, row output.GuestRow, config map[string]any) {
	if row.Node == "" {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "target_start", Status: output.MigrationCheckUnknown, Message: "target guest not located",
		})
		return
	}
	if lock, ok := config["lock"]; ok && strings.TrimSpace(fmt.Sprint(lock)) != "" {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "target_start", Status: output.MigrationCheckFail,
			Message: fmt.Sprintf("target is locked (%s); wait for the lock to clear before starting", fmt.Sprint(lock)),
		})
		return
	}
	switch row.Status {
	case "running":
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "target_start", Status: output.MigrationCheckOK, Message: "target is already running",
		})
	case "stopped":
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "target_start", Status: output.MigrationCheckOK, Message: "target is stopped and not locked; it can be started",
		})
	default:
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "target_start", Status: output.MigrationCheckUnknown, Message: fmt.Sprintf("target state %q unrecognized", row.Status),
		})
	}
}

// checkPostMigrationMAC compares observable MACs: the dangerous case is a
// still-running source with the same hardware address as the target.
// Everything else (DHCP, guest-internal IPs) stays explicitly unknown.
func (s *GuestService) checkPostMigrationMAC(_ context.Context, plan *output.MigrationPlan, sourcePresent bool, sourceRow output.GuestRow, sourceConfig, targetConfig map[string]any) {
	if sourcePresent && sourceRow.Status == "running" && sourceConfig != nil && targetConfig != nil {
		sourceMACs := guestHardwareAddresses(sourceConfig)
		targetMACs := guestHardwareAddresses(targetConfig)
		for mac := range sourceMACs {
			if targetMACs[mac] {
				plan.Checks = append(plan.Checks, output.MigrationCheckRow{
					Check: "mac_ip", Status: output.MigrationCheckFail,
					Message: fmt.Sprintf("duplicate MAC %s on the running source and the target — stop one side before attaching networks", mac),
				})
				return
			}
		}
	}
	plan.Checks = append(plan.Checks, output.MigrationCheckRow{
		Check: "mac_ip", Status: output.MigrationCheckUnknown,
		Message: "no observable duplicate MAC; DHCP leases and guest-internal IPs remain unverifiable via the API — verify connectivity after first boot",
	})
}

// guestHardwareAddresses collects hwaddr= values from net* entries.
func guestHardwareAddresses(config map[string]any) map[string]bool {
	macs := map[string]bool{}
	for key, value := range config {
		text, ok := value.(string)
		if !ok || !strings.HasPrefix(key, "net") {
			continue
		}
		for part := range strings.SplitSeq(text, ",") {
			name, attr, found := strings.Cut(strings.TrimSpace(part), "=")
			if found && strings.TrimSpace(name) == "hwaddr" {
				mac := strings.ToLower(strings.TrimSpace(attr))
				if mac != "" {
					macs[mac] = true
				}
			}
		}
	}
	return macs
}
