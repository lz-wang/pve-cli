package pve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pve-cli/v3/internal/output"
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

// VerifyBackupStorage asserts through the cluster storage configuration
// that a storage exists, is a host-filesystem (dir) storage, and accepts
// backup content. The physical archive path itself is resolved on the host
// with `pvesm path` — which follows the default dump/ directory and any
// content-dirs overrides — so this check only guards the supported scope:
// dir storages with backup content.
func (b *ProxmoxBackend) VerifyBackupStorage(ctx context.Context, storageName string) error {
	storageName = strings.TrimSpace(storageName)
	if storageName == "" {
		return fmt.Errorf("%w: storage is required", ErrInvalid)
	}
	var config struct {
		Type    string          `json:"type"`
		Content json.RawMessage `json:"content"`
	}
	if err := b.client.Get(ctx, "/storage/"+url.PathEscape(storageName), &config); err != nil {
		return ClassifyError(err)
	}
	if config.Type != "dir" {
		return fmt.Errorf("%w: storage %s has type %q; only dir storages are supported for backup copies (PBS must use its native mechanisms)", ErrInvalid, storageName, config.Type)
	}
	contents := parseStorageContentList(config.Content)
	if len(contents) == 0 {
		// An unset content list means PVE does not restrict this storage;
		// the host-side `pvesm path` resolution stays the authoritative
		// check.
		return nil
	}
	for _, content := range contents {
		if strings.TrimSpace(content) == "backup" {
			return nil
		}
	}
	return fmt.Errorf("%w: storage %s does not accept backup content (content: %s); backup copies need a dir storage with backup content", ErrInvalid, storageName, strings.TrimSpace(string(config.Content)))
}

// parseStorageContentList decodes a PVE storage content list, which arrives
// as a comma-separated string or an array depending on the API surface.
func parseStorageContentList(raw json.RawMessage) []string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return strings.Split(single, ",")
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	return nil
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
