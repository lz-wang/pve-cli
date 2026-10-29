package pve

import (
	"context"
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"regexp"
	"sort"
	"strings"
)

type DiskUnlinker interface {
	UnlinkDisk(context.Context, string, int, string) error
}

// UnlinkDisk intentionally never sends force=true. Unused slots are physical deletion.
// The SDK expects a task here, but PVE 9 documents and returns synchronous null.
func (b *ProxmoxBackend) UnlinkDisk(ctx context.Context, node string, id int, slot string) error {
	return ClassifyError(b.client.Put(ctx, guestPath("vm", node, id)+"/unlink", map[string]any{"idlist": slot, "force": false}, nil))
}

type DiskService struct {
	guest   *GuestService
	backend GuestBackend
	tasks   TaskRunner
}

func NewDiskService(b GuestBackend, t TaskRunner) *DiskService {
	return &DiskService{NewVMService(b, t, nil, false), b, t}
}

var attachedSlotPattern = regexp.MustCompile(`^(?:scsi(?:[0-9]|[12][0-9]|30)|virtio(?:[0-9]|1[0-5])|sata[0-5]|ide[0-3])$`)
var unusedSlotPattern = regexp.MustCompile(`^unused(?:[0-9]|[1-9][0-9]|1[0-9][0-9]|2[0-4][0-9]|25[0-5])$`)

func ParseDisks(cfg map[string]any, id int, node string) []output.DiskRow {
	boot := fmt.Sprint(cfg["boot"])
	boot = strings.TrimPrefix(boot, "order=")
	boot = strings.Split(boot, ";")[0]
	if boot == "<nil>" || boot == "" {
		boot = fmt.Sprint(cfg["bootdisk"])
	}
	rows := []output.DiskRow{}
	for slot, value := range cfg {
		if !attachedSlotPattern.MatchString(slot) && !unusedSlotPattern.MatchString(slot) {
			continue
		}
		parts := strings.Split(fmt.Sprint(value), ",")
		if len(parts) == 0 || parts[0] == "none" {
			continue
		}
		r := output.DiskRow{VMID: id, Node: node, Slot: slot, Volume: parts[0], Device: "disk", Role: "attached"}
		if unusedSlotPattern.MatchString(slot) {
			r.Role = "unused"
		}
		if slot == boot {
			r.Role = "system"
		}
		for _, part := range parts[1:] {
			if strings.HasPrefix(part, "size=") {
				r.Size = strings.TrimPrefix(part, "size=")
			}
			if part == "media=cdrom" {
				r.Device = "cdrom"
				r.Role = "installer"
			}
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Slot < rows[j].Slot })
	return rows
}
func (s *DiskService) List(ctx context.Context, id int, node string) ([]output.DiskRow, error) {
	g, err := s.guest.resolve(ctx, id, node)
	if err != nil {
		return nil, err
	}
	b, ok := s.backend.(NativeConfigBackend)
	if !ok {
		return nil, fmt.Errorf("%w: native config unavailable", ErrUnavailable)
	}
	cfg, err := b.GuestConfig(ctx, "vm", g.Row().Node, id)
	if err != nil {
		return nil, err
	}
	return ParseDisks(cfg, id, g.Row().Node), nil
}
func (s *DiskService) Add(ctx context.Context, id int, node, slot, storage, size string) (output.DiskResult, error) {
	result := output.DiskResult{OperationResult: output.OperationResult{Resource: "vm-disk", VMID: id, Node: node, Status: "unknown"}, Slot: slot}
	if !attachedSlotPattern.MatchString(slot) {
		return result, fmt.Errorf("%w: invalid attached disk slot", ErrInvalid)
	}
	gib, err := diskGiB(size)
	if err != nil {
		return result, err
	}
	g, err := s.guest.resolve(ctx, id, node)
	if err != nil {
		return result, err
	}
	node = g.Row().Node
	result.Node = node
	native, ok := s.backend.(NativeConfigBackend)
	if !ok {
		return result, fmt.Errorf("%w: native config unavailable", ErrUnavailable)
	}
	cfg, err := native.GuestConfig(ctx, "vm", node, id)
	if err != nil {
		return result, err
	}
	if _, occupied := cfg[slot]; occupied {
		return result, fmt.Errorf("%w: slot is occupied", ErrConflict)
	}
	if !resourceNamePattern.MatchString(storage) {
		return result, fmt.Errorf("%w: invalid storage", ErrInvalid)
	}

	b, ok := s.backend.(StorageBackend)
	if !ok {
		return result, fmt.Errorf("%w: storage lookup unavailable", ErrUnavailable)
	}
	row, err := b.Storage(ctx, node, storage)
	if err != nil {
		return result, ClassifyError(err)
	}
	if !resourceNamePattern.MatchString(storage) || !storageSupports(row, "images") {
		return result, fmt.Errorf("%w: active images storage required", ErrInvalid)
	}
	task, err := g.Config(ctx, map[string]string{slot: storage + ":" + gib})
	if err != nil {
		return result, ClassifyError(err)
	}
	runner := s.tasks
	runner.Resource = "vm-disk"
	runner.VMID = id
	runner.Node = node
	result.OperationResult, err = runner.Execute(ctx, task)
	if err != nil {
		return result, err
	}
	if result.Status == "succeeded" {
		updated, e := s.List(ctx, id, node)
		if e == nil {
			for _, r := range updated {
				if r.Slot == slot {
					result.Volume = r.Volume
					return result, nil
				}
			}
		}
		result.Status = "unknown"
		return result, fmt.Errorf("%w: disk allocation not verified; task %s", ErrTaskTimeout, result.Task)
	}
	return result, nil
}
func (s *DiskService) Unlink(ctx context.Context, id int, node, slot, expectedVolume string, deleteVolume bool) (output.DiskResult, error) {
	result := output.DiskResult{OperationResult: output.OperationResult{Resource: "vm-disk", VMID: id, Node: node, Status: "unknown"}, Slot: slot}
	if deleteVolume && !unusedSlotPattern.MatchString(slot) || !deleteVolume && !attachedSlotPattern.MatchString(slot) {
		return result, fmt.Errorf("%w: delete requires unusedN; remove requires an attached slot", ErrInvalid)
	}
	rows, err := s.List(ctx, id, node)
	if err != nil {
		return result, err
	}
	for _, r := range rows {
		if r.Slot == slot {
			if r.Device != "disk" {
				return result, fmt.Errorf("%w: only disk devices can be unlinked", ErrInvalid)
			}
			result.Node = r.Node
			result.Volume = r.Volume
		}
	}
	if expectedVolume != "" && result.Volume != expectedVolume {
		return result, fmt.Errorf("%w: disk target changed after confirmation", ErrConflict)
	}
	if result.Volume == "" {
		return result, fmt.Errorf("%w: disk slot %s", ErrNotFound, slot)
	}
	b, ok := s.backend.(DiskUnlinker)
	if !ok {
		return result, fmt.Errorf("%w: unlink unavailable", ErrUnavailable)
	}
	if err = b.UnlinkDisk(ctx, result.Node, id, slot); err != nil {
		return result, err
	}
	updated, err := s.List(ctx, id, result.Node)
	if err != nil {
		return result, fmt.Errorf("%w: unlink submitted; unable to verify", ErrTaskTimeout)
	}
	for _, r := range updated {
		if r.Slot == slot {
			return result, fmt.Errorf("%w: unlink did not remove slot", ErrTaskTimeout)
		}
	}
	if !deleteVolume {
		for _, r := range updated {
			if r.Role == "unused" && r.Volume == result.Volume {
				result.Status = "succeeded"
				return result, nil
			}
		}
		return result, fmt.Errorf("%w: detached volume not found in unused slots", ErrTaskTimeout)
	}
	storage := strings.SplitN(result.Volume, ":", 2)[0]
	reader, ok := s.backend.(StorageBackend)
	if !ok {
		return result, fmt.Errorf("%w: storage verification unavailable", ErrTaskTimeout)
	}
	contents, e := reader.StorageContents(ctx, result.Node, storage)
	if e != nil {
		return result, fmt.Errorf("%w: unable to verify deleted volume", ErrTaskTimeout)
	}
	for _, r := range contents {
		if r.VolID == result.Volume {
			return result, fmt.Errorf("%w: volume still exists", ErrTaskTimeout)
		}
	}
	result.Status = "succeeded"
	return result, nil
}
