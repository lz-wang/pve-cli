package pve

import (
	"context"
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"regexp"
	"sort"
	"strings"
	"time"
)

type BackupAuditBackend interface {
	GuestBackend
	StorageBackend
	BackupReader
}
type BackupAuditOptions struct {
	Node             string
	Kind             GuestType
	IDs              []int
	Tags             []string
	IncludeTemplates bool
	MaxAge           time.Duration
	Now              time.Time
}

func AuditBackups(ctx context.Context, b BackupAuditBackend, o BackupAuditOptions) (output.BackupAuditReport, error) {
	r := output.BackupAuditReport{Complete: true, Rows: []output.BackupAuditRow{}, Errors: []string{}}
	if o.MaxAge <= 0 {
		return r, fmt.Errorf("%w: max-age must be positive", ErrInvalid)
	}
	for _, id := range o.IDs {
		if id <= 0 {
			return r, fmt.Errorf("%w: vmid must be positive", ErrInvalid)
		}
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	nodes, err := b.Nodes(ctx)
	if err != nil {
		return r, err
	}
	issue := func(e error) { r.Complete = false; r.Errors = append(r.Errors, e.Error()) }
	matchedNode := false
	for _, node := range nodes {
		if o.Node != "" && o.Node != node.Name {
			continue
		}
		matchedNode = true
		inventoryComplete := true
		archives := []output.BackupRow{}
		stores, e := b.Storages(ctx, node.Name)
		if e != nil {
			inventoryComplete = false
			issue(fmt.Errorf("%s storage inventory: %w", node.Name, e))
		}
		for _, store := range stores {
			if !contentIncludes(store.Content, "backup") {
				continue
			}
			// The archive reader recognizes vzdump filenames, not PBS snapshot IDs.
			// An unsupported inventory cannot prove that a guest has no backup.
			if store.Type == "pbs" {
				inventoryComplete = false
				issue(fmt.Errorf("%s/%s PBS snapshot coverage is not supported", node.Name, store.Storage))
				continue
			}
			if !store.Active || !store.Enabled {
				inventoryComplete = false
				issue(fmt.Errorf("%s/%s backup storage unavailable", node.Name, store.Storage))
				continue
			}
			rows, e := b.Backups(ctx, node.Name, store.Storage)
			if e != nil {
				inventoryComplete = false
				issue(fmt.Errorf("%s/%s: %w", node.Name, store.Storage, e))
				continue
			}
			archives = append(archives, rows...)
		}
		for _, kind := range []string{"vm", "lxc"} {
			if o.Kind != "" && o.Kind != GuestTypeAll && string(o.Kind) != kind {
				continue
			}
			var guests []output.GuestRow
			if kind == "vm" {
				guests, e = b.VMs(ctx, node.Name)
			} else {
				guests, e = b.LXCs(ctx, node.Name)
			}
			if e != nil {
				issue(fmt.Errorf("%s/%s inventory: %w", node.Name, kind, e))
				continue
			}
			for _, guest := range guests {
				if len(o.IDs) > 0 {
					found := false
					for _, id := range o.IDs {
						if uint64(id) == guest.VMID {
							found = true
						}
					}
					if !found {
						continue
					}
				}
				if len(filterGuestRows([]output.GuestRow{guest}, "", o.Tags, TagMatchAll)) == 0 {
					continue
				}
				row := output.BackupAuditRow{Node: node.Name, Kind: kind, VMID: guest.VMID, Name: guest.Name, Status: "missing", Archives: []output.BackupRow{}, Association: "kind_vmid_candidate", OffHostCopy: "unknown", RestoreVerified: "unknown", Findings: []string{}}
				native, ok := b.(NativeConfigBackend)
				var cfg map[string]any
				if !ok {
					e = fmt.Errorf("native configuration unavailable")
				} else {
					cfg, e = native.GuestConfig(ctx, kind, node.Name, int(guest.VMID))
				}
				configOK := e == nil
				if e != nil {
					row.Findings = append(row.Findings, "configuration unavailable")
					issue(fmt.Errorf("%s/%s/%d configuration unavailable", node.Name, kind, guest.VMID))
				} else {
					if configEnabled(cfg["template"]) && !o.IncludeTemplates {
						continue
					}
					row.Findings = append(row.Findings, backupConfigFindings(cfg)...)
				}
				for _, a := range archives {
					if a.Kind != kind || a.VMID != guest.VMID {
						continue
					}
					row.Archives = append(row.Archives, a)
					if a.CTime > row.LatestCTime {
						row.LatestCTime = a.CTime
					}
				}
				sortBackupRows(row.Archives)
				switch {
				case !inventoryComplete || !configOK:
					row.Status = "unknown"
				case len(row.Archives) == 0:
					row.Status = "missing"
				case row.LatestCTime == 0 || row.LatestCTime > uint64(o.Now.Unix()):
					row.Status = "unknown"
					row.Findings = append(row.Findings, "archive time is missing or in the future")
				case o.Now.Sub(time.Unix(int64(row.LatestCTime), 0)) > o.MaxAge:
					row.Status = "stale"
				default:
					row.Status = "recent_candidate"
				}
				row.Findings = append(row.Findings, "archive identity and application consistency are unverified", "off-host copy and restore-test evidence are unavailable")
				r.Rows = append(r.Rows, row)
			}
		}
	}
	if !matchedNode {
		issue(fmt.Errorf("node %q not found", o.Node))
	}
	for _, id := range o.IDs {
		found := false
		for _, row := range r.Rows {
			if row.VMID == uint64(id) {
				found = true
			}
		}
		if !found {
			issue(fmt.Errorf("selected VMID %d was not included in the audit", id))
		}
	}
	sort.Slice(r.Rows, func(i, j int) bool {
		a, b := r.Rows[i], r.Rows[j]
		if a.Node != b.Node {
			return a.Node < b.Node
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.VMID < b.VMID
	})
	return r, nil
}
func contentIncludes(content, want string) bool {
	for _, s := range strings.Split(content, ",") {
		if strings.TrimSpace(s) == want {
			return true
		}
	}
	return false
}
func configEnabled(v any) bool { s := fmt.Sprint(v); return s == "1" || s == "true" }

var guestDiskKey = regexp.MustCompile(`^(rootfs|mp[0-9]+|scsi[0-9]+|sata[0-9]+|virtio[0-9]+|ide[0-9]+|efidisk0|tpmstate0)$`)

func backupConfigFindings(cfg map[string]any) []string {
	findings := []string{}
	for key, v := range cfg {
		value, ok := v.(string)
		if !ok {
			continue
		}
		if strings.HasPrefix(key, "hostpci") || strings.HasPrefix(key, "usb") || strings.HasPrefix(key, "dev") {
			findings = append(findings, key+": external device requires manual recovery")
			continue
		}
		if !guestDiskKey.MatchString(key) {
			continue
		}
		volume := strings.SplitN(value, ",", 2)[0]
		if strings.HasPrefix(volume, "/") {
			findings = append(findings, key+": host path or device is outside archive protection")
			continue
		}
		if optionAttributeValue(value, "backup") == "0" || strings.HasPrefix(key, "mp") && optionAttributeValue(value, "backup") != "1" {
			findings = append(findings, key+": excluded from backup")
		}
		if optionAttributeValue(value, "media") == "cdrom" {
			findings = append(findings, key+": installation media requires separate availability")
		}
	}
	sort.Strings(findings)
	return findings
}
