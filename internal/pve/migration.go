package pve

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

// CrossProfileMigrationOptions describes a migration between two
// independent PVE hosts/clusters. Target is the target cluster backend; the
// source cluster is the service's own backend.
type CrossProfileMigrationOptions struct {
	TargetNode string
	TargetVMID int
	StorageMap map[string]string
	BridgeMap  map[string]string
	Target     GuestBackend
}

var volumeStoragePattern = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*):`)

// PlanRemoteMigration performs the read-only preflight for a cross-profile
// LXC migration. It never mutates either side. Checks degrade to unknown —
// never to silent safety — when the API cannot observe the facts.
func (s *GuestService) PlanRemoteMigration(ctx context.Context, vmid int, options CrossProfileMigrationOptions) (output.MigrationPlan, error) {
	if s.kind != "lxc" {
		return output.MigrationPlan{}, fmt.Errorf("%w: cross-profile migration is only supported for lxc guests", ErrInvalid)
	}
	guest, err := s.resolve(ctx, vmid, "")
	if err != nil {
		return output.MigrationPlan{}, err
	}
	row := guest.Row()
	plan := output.MigrationPlan{
		Resource:   s.kind,
		VMID:       vmid,
		Node:       row.Node,
		TargetNode: options.TargetNode,
		TargetVMID: options.TargetVMID,
	}
	if plan.TargetVMID == 0 {
		plan.TargetVMID = vmid
	}

	s.checkTargetCluster(ctx, &plan, options)
	s.checkTargetVMID(ctx, &plan, options)

	config := s.readMigrationConfig(ctx, &plan, row.Node, vmid)
	s.checkStorages(ctx, &plan, options, config)
	s.checkNetworks(ctx, &plan, options, config)
	plan.Checks = append(plan.Checks, macIPCheck())
	s.checkOnboot(&plan, config)
	s.checkMounts(&plan, config)
	s.checkSourceState(&plan, row)

	plan.Status = overallMigrationStatus(plan.Checks)
	return plan, nil
}

func (s *GuestService) checkTargetCluster(ctx context.Context, plan *output.MigrationPlan, options CrossProfileMigrationOptions) {
	if options.Target == nil {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "profile", Status: output.MigrationCheckFail, Message: "target profile backend is required",
		})
		return
	}
	nodes, err := options.Target.Nodes(ctx)
	if err != nil {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "profile", Status: output.MigrationCheckFail, Message: fmt.Sprintf("target cluster unreachable: %v", ClassifyError(err)),
		})
		return
	}
	for _, node := range nodes {
		if node.Name == options.TargetNode {
			if node.Status != "online" {
				plan.Checks = append(plan.Checks, output.MigrationCheckRow{
					Check: "profile", Status: output.MigrationCheckFail, Message: fmt.Sprintf("target node %s is %s", options.TargetNode, node.Status),
				})
				return
			}
			plan.Checks = append(plan.Checks, output.MigrationCheckRow{
				Check: "profile", Status: output.MigrationCheckOK, Message: fmt.Sprintf("target node %s online", options.TargetNode),
			})
			return
		}
	}
	plan.Checks = append(plan.Checks, output.MigrationCheckRow{
		Check: "profile", Status: output.MigrationCheckFail,
		Message: fmt.Sprintf("target node %s not found (nodes: %s)", options.TargetNode, nodeNameList(nodes)),
	})
}

func (s *GuestService) checkTargetVMID(ctx context.Context, plan *output.MigrationPlan, options CrossProfileMigrationOptions) {
	nodes, err := options.Target.Nodes(ctx)
	if err != nil {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "vmid", Status: output.MigrationCheckUnknown, Message: "cannot enumerate target guests",
		})
		return
	}
	takenOn := ""
	for _, node := range nodes {
		for _, guestRow := range targetGuests(ctx, options.Target, node.Name) {
			if int(guestRow.VMID) == plan.TargetVMID {
				takenOn = fmt.Sprintf("vmid %d already exists on target node %s", plan.TargetVMID, node.Name)
				break
			}
		}
		if takenOn != "" {
			break
		}
	}
	if takenOn != "" {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "vmid", Status: output.MigrationCheckFail, Message: takenOn + "; choose --target-vmid",
		})
		return
	}
	plan.Checks = append(plan.Checks, output.MigrationCheckRow{
		Check: "vmid", Status: output.MigrationCheckOK, Message: fmt.Sprintf("vmid %d free on target cluster", plan.TargetVMID),
	})
}

func targetGuests(ctx context.Context, target GuestBackend, node string) []output.GuestRow {
	vms, vmErr := target.VMs(ctx, node)
	lxcs, lxcErr := target.LXCs(ctx, node)
	if vmErr != nil || lxcErr != nil {
		return nil
	}
	return append(vms, lxcs...)
}

// readMigrationConfig fetches the source guest configuration; dependent
// checks degrade to unknown without it.
func (s *GuestService) readMigrationConfig(ctx context.Context, plan *output.MigrationPlan, node string, vmid int) map[string]any {
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

func (s *GuestService) checkStorages(ctx context.Context, plan *output.MigrationPlan, options CrossProfileMigrationOptions, config map[string]any) {
	if config == nil {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "storage", Status: output.MigrationCheckUnknown, Message: "source configuration unreadable; storage requirements unknown",
		})
		return
	}
	volumes := sourceVolumes(config)
	if len(volumes) == 0 {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "storage", Status: output.MigrationCheckUnknown, Message: "no volume-bearing entries found in source configuration",
		})
		return
	}
	storageBackend, ok := options.Target.(StorageBackend)
	if !ok {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "storage", Status: output.MigrationCheckUnknown, Message: "target storage inventory unavailable",
		})
		return
	}
	rows, err := storageBackend.Storages(ctx, options.TargetNode)
	if err != nil {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "storage", Status: output.MigrationCheckFail, Message: fmt.Sprintf("target storage inventory: %v", ClassifyError(err)),
		})
		return
	}
	available := make(map[string]output.StorageRow)
	for _, row := range rows {
		available[row.Storage] = row
	}

	failures := []string{}
	totalBytes := uint64(0)
	names := make([]string, 0, len(volumes))
	for name := range volumes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		spec := volumes[name]
		sourceStorage := spec.storage
		targetStorage := sourceStorage
		if mapped, ok := options.StorageMap[sourceStorage]; ok {
			targetStorage = mapped
		}
		if spec.hasSize {
			totalBytes += spec.size
		}
		row, ok := available[targetStorage]
		if !ok {
			failures = append(failures, fmt.Sprintf("%s: storage %q not present on target node %s (map %s->%s)", name, targetStorage, options.TargetNode, sourceStorage, targetStorage))
			continue
		}
		if !row.Active || !row.Enabled {
			failures = append(failures, fmt.Sprintf("%s: storage %q is not active on target", name, targetStorage))
		}
	}
	if len(failures) > 0 {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "storage", Status: output.MigrationCheckFail, Message: strings.Join(failures, "; "),
		})
		return
	}
	if totalBytes > 0 {
		// Sum the availability of every involved target storage once.
		availTotal := uint64(0)
		seen := map[string]bool{}
		for _, name := range names {
			sourceStorage := volumes[name].storage
			targetStorage := sourceStorage
			if mapped, ok := options.StorageMap[sourceStorage]; ok {
				targetStorage = mapped
			}
			if seen[targetStorage] {
				continue
			}
			seen[targetStorage] = true
			availTotal += available[targetStorage].Avail
		}
		if availTotal < totalBytes {
			plan.Checks = append(plan.Checks, output.MigrationCheckRow{
				Check: "storage", Status: output.MigrationCheckFail,
				Message: fmt.Sprintf("target capacity insufficient: need ~%s, target has %s available", output.FormatBytes(totalBytes), output.FormatBytes(availTotal)),
			})
			return
		}
	}
	plan.Checks = append(plan.Checks, output.MigrationCheckRow{
		Check: "storage", Status: output.MigrationCheckOK,
		Message: fmt.Sprintf("%d volume(s) mapped to active target storages", len(volumes)),
	})
}

// volumeSpec describes one volume-bearing configuration entry.
type volumeSpec struct {
	storage string
	size    uint64
	hasSize bool
}

// sourceVolumes extracts storage names and declared sizes from rootfs/mpN
// entries ("local-lvm:vm-101-disk-0,size=8G").
func sourceVolumes(config map[string]any) map[string]volumeSpec {
	volumes := map[string]volumeSpec{}
	for key, value := range config {
		if key != "rootfs" && !strings.HasPrefix(key, "mp") {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		// Bind mounts (mp=/host/path) have no storage prefix.
		match := volumeStoragePattern.FindStringSubmatch(text)
		if match == nil {
			continue
		}
		entry := volumeSpec{storage: match[1]}
		if size, ok := optionSize(text); ok {
			entry.size = size
			entry.hasSize = true
		}
		volumes[key] = entry
	}
	return volumes
}

// optionSize parses ",size=8G"-style suffixes into bytes.
func optionSize(value string) (uint64, bool) {
	for part := range strings.SplitSeq(value, ",") {
		name, attr, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found || strings.TrimSpace(name) != "size" {
			continue
		}
		return parseSizeBytes(strings.TrimSpace(attr))
	}
	return 0, false
}

func parseSizeBytes(text string) (uint64, bool) {
	if text == "" {
		return 0, false
	}
	unit := byte(text[len(text)-1])
	multiplier := uint64(1)
	number := text
	switch unit {
	case 'K', 'k':
		multiplier = 1024
		number = text[:len(text)-1]
	case 'M', 'm':
		multiplier = 1024 * 1024
		number = text[:len(text)-1]
	case 'G', 'g':
		multiplier = 1024 * 1024 * 1024
		number = text[:len(text)-1]
	case 'T', 't':
		multiplier = 1024 * 1024 * 1024 * 1024
		number = text[:len(text)-1]
	}
	value, err := strconv.ParseFloat(number, 64)
	if err != nil || value < 0 {
		return 0, false
	}
	return uint64(value * float64(multiplier)), true
}

func (s *GuestService) checkNetworks(ctx context.Context, plan *output.MigrationPlan, options CrossProfileMigrationOptions, config map[string]any) {
	if config == nil {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "network", Status: output.MigrationCheckUnknown, Message: "source configuration unreadable; bridge requirements unknown",
		})
		return
	}
	bridges := map[string][]string{} // bridge -> devices
	for key, value := range config {
		if !strings.HasPrefix(key, "net") {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		for part := range strings.SplitSeq(text, ",") {
			name, attr, found := strings.Cut(strings.TrimSpace(part), "=")
			if found && strings.TrimSpace(name) == "bridge" {
				bridges[strings.TrimSpace(attr)] = append(bridges[strings.TrimSpace(attr)], key)
				break
			}
		}
	}
	if len(bridges) == 0 {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "network", Status: output.MigrationCheckUnknown, Message: "no bridged interfaces found; verify connectivity manually",
		})
		return
	}
	networkBackend, ok := options.Target.(NetworkBackend)
	if !ok {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "network", Status: output.MigrationCheckUnknown, Message: "target network inventory unavailable",
		})
		return
	}
	rows, err := networkBackend.Networks(ctx, options.TargetNode, NetworkListOptions{})
	if err != nil {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "network", Status: output.MigrationCheckFail, Message: fmt.Sprintf("target network inventory: %v", ClassifyError(err)),
		})
		return
	}
	existing := map[string]bool{}
	for _, row := range rows {
		existing[row.Name] = true
	}
	var failures []string
	names := make([]string, 0, len(bridges))
	for name := range bridges {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, sourceBridge := range names {
		targetBridge := sourceBridge
		if mapped, ok := options.BridgeMap[sourceBridge]; ok {
			targetBridge = mapped
		}
		if !existing[targetBridge] {
			failures = append(failures, fmt.Sprintf("%s (devices %s): bridge %q missing on target node %s", sourceBridge, strings.Join(bridges[sourceBridge], ","), targetBridge, options.TargetNode))
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
		Message: fmt.Sprintf("%d bridge(s) present on target", len(bridges)),
	})
}

// macIPCheck documents what the API cannot see: DHCP-assigned or manually
// configured guest IPs and duplicate MACs are only partially observable.
func macIPCheck() output.MigrationCheckRow {
	return output.MigrationCheckRow{
		Check:   "mac_ip",
		Status:  output.MigrationCheckUnknown,
		Message: "MAC/IP conflicts across clusters are not reliably observable via the API; DHCP leases and guest-internal IPs may conflict after migration — verify after first boot",
	}
}

func (s *GuestService) checkOnboot(plan *output.MigrationPlan, config map[string]any) {
	if config == nil {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "onboot", Status: output.MigrationCheckUnknown, Message: "source configuration unreadable",
		})
		return
	}
	if value, ok := config["onboot"]; ok && configBool(value) {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "onboot", Status: output.MigrationCheckWarning,
			Message: "onboot=1: the guest starts automatically on the target after migration; plan the maintenance window and disable first if unwanted",
		})
		return
	}
	plan.Checks = append(plan.Checks, output.MigrationCheckRow{
		Check: "onboot", Status: output.MigrationCheckOK, Message: "guest does not autostart",
	})
}

func (s *GuestService) checkMounts(plan *output.MigrationPlan, config map[string]any) {
	if config == nil {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "mount", Status: output.MigrationCheckUnknown, Message: "source configuration unreadable",
		})
		return
	}
	var findings []string
	for key, value := range config {
		text, ok := value.(string)
		if !ok {
			continue
		}
		switch {
		case strings.HasPrefix(key, "mp") && volumeStoragePattern.MatchString(text):
			// Volume mounts are covered by the storage checks.
		case strings.HasPrefix(key, "mp"):
			findings = append(findings, fmt.Sprintf("%s bind mount %q depends on host paths", key, text))
		case strings.HasPrefix(key, "dev"):
			findings = append(findings, fmt.Sprintf("%s device passthrough %q requires the device on the target host", key, text))
		}
	}
	if len(findings) == 0 {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "mount", Status: output.MigrationCheckOK, Message: "no bind mounts or device passthrough",
		})
		return
	}
	sort.Strings(findings)
	plan.Checks = append(plan.Checks, output.MigrationCheckRow{
		Check: "mount", Status: output.MigrationCheckWarning,
		Message: strings.Join(findings, "; ") + "; these require manual handling on the target",
	})
}

func (s *GuestService) checkSourceState(plan *output.MigrationPlan, row output.GuestRow) {
	switch row.Status {
	case "running":
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "state", Status: output.MigrationCheckWarning,
			Message: "guest is running; the default offline migration requires stopping it first — expect a downtime window",
		})
	case "stopped":
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "state", Status: output.MigrationCheckOK, Message: "guest is stopped; ready for offline migration",
		})
	default:
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{
			Check: "state", Status: output.MigrationCheckUnknown, Message: fmt.Sprintf("guest state %q unrecognized", row.Status),
		})
	}
}

func overallMigrationStatus(checks []output.MigrationCheckRow) string {
	status := output.MigrationCheckOK
	for _, row := range checks {
		switch row.Status {
		case output.MigrationCheckFail:
			return "failed"
		case output.MigrationCheckWarning, output.MigrationCheckUnknown:
			status = "warning"
		}
	}
	return status
}

func configBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := parseConfigBool(typed)
		return err == nil && parsed
	case float64:
		return typed != 0
	default:
		return false
	}
}

func nodeNameList(rows []output.NodeRow) string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	if len(names) == 0 {
		return "none visible"
	}
	return strings.Join(names, ",")
}
