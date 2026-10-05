package pve

import (
	"context"
	"fmt"
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

func (b *ProxmoxBackend) Storages(ctx context.Context, nodeName string) ([]output.StorageRow, error) {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return nil, fmt.Errorf("node is required")
	}

	node, err := b.client.Node(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	storages, err := node.Storages(ctx)
	if err != nil {
		return nil, err
	}

	rows := make([]output.StorageRow, 0, len(storages))
	for _, storage := range storages {
		rows = append(rows, storageRow(storage))
	}
	return rows, nil
}

func (b *ProxmoxBackend) Storage(ctx context.Context, nodeName, storageName string) (output.StorageRow, error) {
	nodeName = strings.TrimSpace(nodeName)
	storageName = strings.TrimSpace(storageName)
	if nodeName == "" {
		return output.StorageRow{}, fmt.Errorf("node is required")
	}
	if storageName == "" {
		return output.StorageRow{}, fmt.Errorf("storage is required")
	}

	node, err := b.client.Node(ctx, nodeName)
	if err != nil {
		return output.StorageRow{}, err
	}
	storage, err := node.Storage(ctx, storageName)
	if err != nil {
		return output.StorageRow{}, err
	}
	return storageRow(storage), nil
}

func (b *ProxmoxBackend) StorageContents(ctx context.Context, nodeName, storageName string) ([]output.StorageContentRow, error) {
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

	rows := make([]output.StorageContentRow, 0, len(items))
	for _, item := range items {
		rows = append(rows, storageContentRow(nodeName, storageName, item))
	}
	return rows, nil
}

func storageContentRow(nodeName, storageName string, item *proxmox.StorageContent) output.StorageContentRow {
	if item == nil {
		return output.StorageContentRow{
			Node:    nodeName,
			Storage: storageName,
			Content: "unknown",
		}
	}
	return output.StorageContentRow{
		Node:        nodeName,
		Storage:     storageName,
		Content:     inferStorageContentType(item.Volid),
		VMID:        inferStorageContentVMID(item.VMID, item.Volid),
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
