package pve

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

func (b *ProxmoxBackend) Backups(ctx context.Context, nodeName, storageName string) ([]output.BackupRow, error) {
	nodeName = strings.TrimSpace(nodeName)
	storageName = strings.TrimSpace(storageName)
	if nodeName == "" {
		return nil, fmt.Errorf("node is required")
	}
	if storageName == "" {
		return nil, fmt.Errorf("storage is required")
	}

	node, err := b.client.Node(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	storage, err := node.Storage(ctx, storageName)
	if err != nil {
		return nil, err
	}
	items, err := storage.GetContent(ctx)
	if err != nil {
		return nil, err
	}

	rows := make([]output.BackupRow, 0, len(items))
	for _, item := range items {
		row := mapStorageContentToBackupRow(nodeName, storageName, item)
		if row.Kind != BackupKindVM && row.Kind != BackupKindLXC {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (b *ProxmoxBackend) BackupGuest(ctx context.Context, nodeName string, options BackupOptions) (Task, error) {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return nil, fmt.Errorf("node is required")
	}
	if options.VMID <= 0 {
		return nil, fmt.Errorf("invalid vmid %d", options.VMID)
	}
	if strings.TrimSpace(options.Storage) == "" {
		return nil, fmt.Errorf("storage is required")
	}
	mode, err := ParseBackupMode(options.Mode)
	if err != nil {
		return nil, err
	}
	compress, err := ParseBackupCompress(options.Compress)
	if err != nil {
		return nil, err
	}
	protected, err := ParseBackupProtected(options.Protected)
	if err != nil {
		return nil, err
	}

	node, err := b.client.Node(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	params := &proxmox.VirtualMachineBackupOptions{
		VMID:     uint64(options.VMID),
		Storage:  options.Storage,
		Mode:     proxmox.VirtualMachineBackupMode(mode),
		Compress: proxmox.VirtualMachineBackupCompress(mapBackupCompress(compress)),
	}
	if options.NotesTemplate != "" {
		params.NotesTemplate = options.NotesTemplate
	}
	if options.BwLimit > 0 {
		params.BwLimit = options.BwLimit
	}
	if protected != "" {
		params.Protected = protected
	}

	task, err := node.Vzdump(ctx, params)
	return wrapTask(task), err
}

func mapStorageContentToBackupRow(nodeName, storageName string, item *proxmox.StorageContent) output.BackupRow {
	if item == nil {
		return output.BackupRow{Node: nodeName, Storage: storageName, Kind: "unknown"}
	}
	kind := inferBackupKind(item.Volid)
	return output.BackupRow{
		Node:        nodeName,
		Storage:     storageName,
		Kind:        kind,
		VMID:        inferBackupVMID(item.VMID, item.Volid),
		VolID:       item.Volid,
		Format:      item.Format,
		Size:        item.Size,
		Used:        item.Used,
		CTime:       uint64(item.Ctime),
		Protected:   formatBackupProtection(item.Protection),
		Encrypted:   item.Encrypted,
		VerifyState: storageVerificationState(item.Verification),
		Notes:       item.Notes,
	}
}

func inferBackupKind(volid string) string {
	base := path.Base(volid)
	switch {
	case strings.Contains(base, "vzdump-qemu-"):
		return BackupKindVM
	case strings.Contains(base, "vzdump-lxc-"):
		return BackupKindLXC
	default:
		return "unknown"
	}
}

func inferBackupVMID(vmid uint64, volid string) uint64 {
	if vmid != 0 {
		return vmid
	}
	base := path.Base(volid)
	for _, prefix := range []string{"vzdump-qemu-", "vzdump-lxc-"} {
		idx := strings.Index(base, prefix)
		if idx < 0 {
			continue
		}
		rest := base[idx+len(prefix):]
		id, _, _ := strings.Cut(rest, "-")
		parsed, err := strconv.ParseUint(id, 10, 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func formatBackupProtection(value proxmox.IntOrBool) string {
	if bool(value) {
		return "1"
	}
	return ""
}

func storageVerificationState(value *proxmox.StorageContentVerification) string {
	if value == nil {
		return ""
	}
	return value.State
}

func mapBackupCompress(value string) string {
	if value == BackupCompressNone {
		return string(proxmox.VirtualMachineBackupCompressZero)
	}
	return value
}
