package pve

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

// ArchiveConfigReader reads the configuration stored inside an archive.
type ArchiveConfigReader interface {
	ArchiveConfig(context.Context, string, string) (map[string]any, error)
}

func (b *ProxmoxBackend) ArchiveConfig(ctx context.Context, node, archive string) (map[string]any, error) {
	var raw string
	query := url.Values{"volume": {archive}}
	if err := b.client.Get(ctx, "/nodes/"+url.PathEscape(node)+"/vzdump/extractconfig?"+query.Encode(), &raw); err != nil {
		return nil, ClassifyError(err)
	}
	return parseArchiveConfig(raw)
}

func parseArchiveConfig(raw string) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, fmt.Errorf("%w: empty or oversized archive configuration", ErrInvalid)
	}
	cfg := map[string]any{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			break
		} // Snapshot sections are not the restored current configuration.
		key, value, ok := strings.Cut(line, ":")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("%w: malformed archive configuration", ErrInvalid)
		}
		if _, exists := cfg[key]; exists {
			return nil, fmt.Errorf("%w: duplicate archive configuration key", ErrInvalid)
		}
		cfg[key] = strings.TrimSpace(value)
	}
	if len(cfg) == 0 {
		return nil, fmt.Errorf("%w: no archive configuration fields", ErrInvalid)
	}
	return cfg, nil
}

// ArchivePlanOptions selects an archive and an independent restore destination.
type ArchivePlanOptions struct {
	SourceNode string
	RestoreOptions
	DiskStorage map[string]string
}

type ArchivePlanBackend interface {
	RestoreBackend
	StorageBackend
	NetworkBackend
}

// PlanArchiveRestore reads both sides. It does not transfer an archive or reserve capacity.
func PlanArchiveRestore(ctx context.Context, source BackupReader, target ArchivePlanBackend, o ArchivePlanOptions) (output.MigrationPlan, error) {
	plan := output.MigrationPlan{Resource: o.Kind, Node: o.SourceNode, TargetNode: o.Node, TargetVMID: o.VMID, Checks: []output.MigrationCheckRow{}}
	add := func(check, status, message string) {
		plan.Checks = append(plan.Checks, output.MigrationCheckRow{Check: check, Status: status, Message: message})
	}
	if _, err := parseRestoreKind(o.Kind); err != nil {
		return plan, err
	}
	if o.SourceNode == "" || o.Node == "" || o.VMID <= 0 {
		return plan, fmt.Errorf("%w: source node, target node and positive target VMID are required", ErrInvalid)
	}
	storage, _, err := parseBackupVolid(o.Archive)
	if err != nil {
		return plan, err
	}
	if err = checkRestoreArchiveKind(o.Kind, o.Archive); err != nil {
		return plan, err
	}
	if err = NewRestoreService(target, TaskRunner{}).VMIDFree(ctx, o.VMID); err != nil {
		add("vmid", "fail", err.Error())
	} else {
		add("vmid", "ok", "target VMID is free across the destination cluster")
	}
	nodes, err := target.Nodes(ctx)
	foundNode := false
	for _, n := range nodes {
		if n.Name == o.Node {
			foundNode = true
		}
	}
	if err != nil || !foundNode {
		add("target_node", "fail", "target node inventory unavailable or target node missing")
	}
	rows, err := source.Backups(ctx, o.SourceNode, storage)
	found := false
	for _, r := range rows {
		if r.VolID == o.Archive && r.Kind == o.Kind {
			found = true
			plan.VMID = int(r.VMID)
		}
	}
	var cfg map[string]any
	if err != nil || !found {
		add("archive", "fail", "archive inventory unavailable or exact archive not found")
	} else if reader, ok := source.(ArchiveConfigReader); !ok {
		add("archive_config", "unknown", "archive configuration reader unavailable")
	} else {
		cfg, err = reader.ArchiveConfig(ctx, o.SourceNode, o.Archive)
		if err != nil {
			add("archive_config", "unknown", fmt.Sprintf("cannot read archive configuration: %v", err))
		} else {
			add("archive_config", "ok", "read configuration from the source archive")
		}
	}
	if cfg != nil {
		planArchiveDisks(ctx, target, o, cfg, add)
		planArchiveNetworks(ctx, target, o, cfg, add)
		for _, finding := range backupConfigFindings(cfg) {
			add("external_data", "warning", finding)
		}
		for _, key := range []string{"cpu", "bios", "machine", "efidisk0", "tpmstate0", "onboot"} {
			if _, ok := cfg[key]; ok {
				add(key, "warning", "archive contains this setting; verify target compatibility before starting the restored guest")
			}
		}
	}
	add("capacity", "unknown", "declared virtual disk sizes exclude storage metadata and temporary space; this check does not reserve capacity")
	add("identity", "warning", "MAC addresses, IP addresses and application identity remain in the archive; isolate the restored guest before starting it")
	add("recovery", "unknown", "boot, application consistency and external data recovery require a separate restore test")
	plan.Status = overallMigrationStatus(plan.Checks)
	return plan, nil
}

var archiveNetKey = regexp.MustCompile(`^net[0-9]+$`)

func planArchiveDisks(ctx context.Context, b StorageBackend, o ArchivePlanOptions, cfg map[string]any, add func(string, string, string)) {
	rows, err := b.Storages(ctx, o.Node)
	if err != nil {
		add("storage", "unknown", "target storage inventory unavailable")
		return
	}
	byName := map[string]output.StorageRow{}
	for _, r := range rows {
		byName[r.Storage] = r
	}
	keys := make([]string, 0)
	for k := range cfg {
		if guestDiskKey.MatchString(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	totals := map[string]uint64{}
	seen := map[string]bool{}
	unknown := map[string]bool{}
	for _, key := range keys {
		value := fmt.Sprint(cfg[key])
		volume := strings.Split(value, ",")[0]
		if strings.HasPrefix(volume, "/") || volume == "none" || strings.Contains(value, "media=cdrom") {
			continue
		}
		if optionAttributeValue(value, "backup") == "0" || (strings.HasPrefix(key, "mp") && optionAttributeValue(value, "backup") != "1") {
			continue
		}
		target := o.Storage
		if v, ok := o.DiskStorage[key]; ok {
			target = v
			seen[key] = true
		}
		if target == "" {
			target, _, _ = strings.Cut(volume, ":")
		}
		row, ok := byName[target]
		content := "images"
		if o.Kind == "lxc" {
			content = "rootdir"
		}
		if !ok || !row.Enabled || !row.Active || !contentIncludes(row.Content, content) {
			add("disk."+key, "fail", fmt.Sprintf("target storage %s is missing, inactive or does not support %s", target, content))
			continue
		}
		size, valid := safeArchiveDiskSize(value)
		if !valid {
			unknown[target] = true
			add("disk."+key, "unknown", fmt.Sprintf("target %s; archive does not declare a valid disk size", target))
			continue
		}
		if math.MaxUint64-totals[target] < size {
			unknown[target] = true
			add("disk."+key, "fail", "sum of declared disk sizes overflows")
			continue
		}
		totals[target] += size
		add("disk."+key, "ok", fmt.Sprintf("target %s; declared virtual size %d bytes", target, size))
	}
	for key := range o.DiskStorage {
		if !seen[key] {
			add("disk_map."+key, "fail", "mapping does not select a backed-up disk")
		}
	}
	names := make([]string, 0, len(totals))
	for name := range totals {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		status := "ok"
		if totals[name] > byName[name].Avail {
			status = "fail"
		} else if unknown[name] {
			status = "unknown"
		}
		add("space."+name, status, fmt.Sprintf("declared disks need %d bytes; storage reports %d available bytes", totals[name], byName[name].Avail))
	}
	if len(totals) == 0 {
		add("disk_sizes", "unknown", "no backed-up disk size could be established")
	}
	if len(o.DiskStorage) > 0 {
		add("disk_mapping", "warning", "per-disk mapping is planning only; existing restore commands accept one destination storage")
	}
}
func safeArchiveDiskSize(value string) (uint64, bool) {
	raw := optionAttributeValue(value, "size")
	if !regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?[KMGT]?$`).MatchString(strings.ToUpper(raw)) {
		return 0, false
	}
	size, ok := parseSizeBytes(raw)
	// parseSizeBytes uses float64; bound the conversion before accepting it.
	if !ok || size == 0 || size >= math.MaxInt64 {
		return 0, false
	}
	return size, true
}
func planArchiveNetworks(ctx context.Context, b NetworkBackend, o ArchivePlanOptions, cfg map[string]any, add func(string, string, string)) {
	rows, err := b.Networks(ctx, o.Node, NetworkListOptions{})
	if err != nil {
		add("network", "unknown", "target network inventory unavailable")
		return
	}
	bridges := map[string]bool{}
	for _, r := range rows {
		if r.Type == "bridge" || r.Type == "OVSBridge" {
			bridges[r.Name] = true
		}
	}
	seen := map[string]bool{}
	keys := []string{}
	for k := range cfg {
		if archiveNetKey.MatchString(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		src := optionAttributeValue(fmt.Sprint(cfg[key]), "bridge")
		if src == "" {
			add("network."+key, "unknown", "interface has no declared bridge")
			continue
		}
		dst := src
		if v, ok := o.BridgeMap[src]; ok {
			dst = v
			seen[src] = true
		}
		status := "ok"
		if !bridges[dst] {
			status = "fail"
		}
		add("network."+key, status, fmt.Sprintf("bridge %s -> %s", src, dst))
	}
	for src := range o.BridgeMap {
		if !seen[src] {
			add("bridge_map."+src, "fail", "source bridge does not occur in the archive")
		}
	}
}
