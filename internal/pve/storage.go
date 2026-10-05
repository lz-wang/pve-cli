package pve

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

type StorageListOptions struct {
	Node    string
	Content string
	Type    string
	Active  bool
	Enabled bool
}

type StorageContentListOptions struct {
	Node    string
	Storage string
	Content string
	VMID    int
}

type StorageService struct {
	backend StorageBackend
}

func NewStorageService(backend StorageBackend) *StorageService {
	return &StorageService{backend: backend}
}

func (s *StorageService) List(ctx context.Context, options StorageListOptions) ([]output.StorageRow, error) {
	node := strings.TrimSpace(options.Node)
	if node != "" {
		rows, err := s.backend.Storages(ctx, node)
		if err != nil {
			return nil, err
		}
		return sortStorageRows(filterStorageRows(rows, options)), nil
	}

	nodes, err := s.backend.Nodes(ctx)
	if err != nil {
		return nil, err
	}

	rows := make([]output.StorageRow, 0)
	successes := 0
	var firstErr error
	for _, nodeRow := range nodes {
		if nodeRow.Name == "" {
			continue
		}
		nodeRows, err := s.backend.Storages(ctx, nodeRow.Name)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		successes++
		rows = append(rows, nodeRows...)
	}

	if successes == 0 && firstErr != nil {
		return nil, fmt.Errorf("list storage: no nodes could be queried: %w", firstErr)
	}
	return sortStorageRows(filterStorageRows(rows, options)), nil
}

func (s *StorageService) Get(ctx context.Context, node, storage string) (output.StorageRow, error) {
	node = strings.TrimSpace(node)
	storage = strings.TrimSpace(storage)
	if node == "" {
		return output.StorageRow{}, fmt.Errorf("node is required")
	}
	if storage == "" {
		return output.StorageRow{}, fmt.Errorf("storage is required")
	}
	return s.backend.Storage(ctx, node, storage)
}

func (s *StorageService) ListContent(ctx context.Context, options StorageContentListOptions) ([]output.StorageContentRow, error) {
	node := strings.TrimSpace(options.Node)
	storage := strings.TrimSpace(options.Storage)
	if node == "" {
		return nil, fmt.Errorf("node is required")
	}
	if storage == "" {
		return nil, fmt.Errorf("storage is required")
	}
	if options.VMID < 0 {
		return nil, fmt.Errorf("invalid vmid %d", options.VMID)
	}

	rows, err := s.backend.StorageContents(ctx, node, storage)
	if err != nil {
		return nil, err
	}
	return sortStorageContentRows(filterStorageContentRows(rows, options)), nil
}

func filterStorageRows(rows []output.StorageRow, options StorageListOptions) []output.StorageRow {
	content := strings.ToLower(strings.TrimSpace(options.Content))
	storageType := strings.ToLower(strings.TrimSpace(options.Type))
	if content == "" && storageType == "" && !options.Active && !options.Enabled {
		return rows
	}

	out := rows[:0]
	for _, row := range rows {
		if content != "" && !storageHasContent(row.Content, content) {
			continue
		}
		if storageType != "" && strings.ToLower(strings.TrimSpace(row.Type)) != storageType {
			continue
		}
		if options.Active && !row.Active {
			continue
		}
		if options.Enabled && !row.Enabled {
			continue
		}
		out = append(out, row)
	}
	return out
}

func filterStorageContentRows(rows []output.StorageContentRow, options StorageContentListOptions) []output.StorageContentRow {
	content := strings.ToLower(strings.TrimSpace(options.Content))
	if content == "" && options.VMID <= 0 {
		return rows
	}

	out := rows[:0]
	for _, row := range rows {
		if content != "" && strings.ToLower(strings.TrimSpace(row.Content)) != content {
			continue
		}
		if options.VMID > 0 && row.VMID != uint64(options.VMID) {
			continue
		}
		out = append(out, row)
	}
	return out
}

func sortStorageRows(rows []output.StorageRow) []output.StorageRow {
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Node != b.Node {
			return a.Node < b.Node
		}
		return a.Storage < b.Storage
	})
	return rows
}

func sortStorageContentRows(rows []output.StorageContentRow) []output.StorageContentRow {
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Content != b.Content {
			return a.Content < b.Content
		}
		if a.VMID != b.VMID {
			return a.VMID < b.VMID
		}
		if a.CTime != b.CTime {
			return a.CTime > b.CTime
		}
		return a.VolID < b.VolID
	})
	return rows
}

func storageHasContent(content, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return true
	}
	for _, part := range strings.Split(content, ",") {
		if strings.ToLower(strings.TrimSpace(part)) == want {
			return true
		}
	}
	return false
}

func inferStorageContentType(volid string) string {
	switch {
	case strings.Contains(volid, ":iso/"):
		return "iso"
	case strings.Contains(volid, ":vztmpl/"):
		return "vztmpl"
	case strings.Contains(volid, ":backup/"):
		return "backup"
	case strings.Contains(volid, ":snippets/"):
		return "snippets"
	case strings.Contains(volid, ":import/"):
		return "import"
	case strings.Contains(volid, ":subvol-"):
		return "rootdir"
	case strings.Contains(volid, ":vm-") || strings.Contains(volid, ":base-"):
		return "images"
	default:
		return "unknown"
	}
}

func inferStorageContentVMID(vmid uint64, volid string) uint64 {
	if vmid != 0 {
		return vmid
	}

	for _, prefix := range []string{"vm-", "base-", "subvol-", "vzdump-qemu-", "vzdump-lxc-"} {
		if parsed := parseIDAfterPrefix(volid, prefix); parsed != 0 {
			return parsed
		}
	}
	return 0
}

func parseIDAfterPrefix(value, prefix string) uint64 {
	idx := strings.Index(value, prefix)
	if idx < 0 {
		return 0
	}
	rest := value[idx+len(prefix):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	parsed, err := strconv.ParseUint(rest[:end], 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}
