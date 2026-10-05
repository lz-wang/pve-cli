package pve

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

const (
	BackupKindAll = "all"
	BackupKindVM  = "vm"
	BackupKindLXC = "lxc"

	BackupModeSnapshot = "snapshot"
	BackupModeSuspend  = "suspend"
	BackupModeStop     = "stop"

	BackupCompressZstd = "zstd"
	BackupCompressLzo  = "lzo"
	BackupCompressGzip = "gzip"
	BackupCompressNone = "none"
)

type BackupService struct {
	backend BackupBackend
	tasks   TaskRunner
}

func NewBackupService(backend BackupBackend, tasks TaskRunner) *BackupService {
	return &BackupService{backend: backend, tasks: tasks}
}

type BackupListOptions struct {
	Node    string
	Storage string
	VMID    int
	Kind    string
	Latest  bool
}

type BackupCreateOptions struct {
	Kind          string
	VMID          int
	Node          string
	Storage       string
	Mode          string
	Compress      string
	NotesTemplate string
	BwLimit       uint
	Protected     string
}

func (s *BackupService) List(ctx context.Context, options BackupListOptions) ([]output.BackupRow, error) {
	if strings.TrimSpace(options.Node) == "" {
		return nil, fmt.Errorf("node is required")
	}
	if strings.TrimSpace(options.Storage) == "" {
		return nil, fmt.Errorf("storage is required")
	}
	if options.VMID < 0 {
		return nil, fmt.Errorf("invalid vmid %d", options.VMID)
	}

	kind, err := ParseBackupKind(options.Kind)
	if err != nil {
		return nil, err
	}

	rows, err := s.backend.Backups(ctx, options.Node, options.Storage)
	if err != nil {
		return nil, err
	}

	filtered := make([]output.BackupRow, 0, len(rows))
	for _, row := range rows {
		if row.Kind != BackupKindVM && row.Kind != BackupKindLXC {
			continue
		}
		if options.VMID > 0 && row.VMID != uint64(options.VMID) {
			continue
		}
		if kind != BackupKindAll && row.Kind != kind {
			continue
		}
		filtered = append(filtered, row)
	}

	if options.Latest {
		filtered = latestBackups(filtered)
	}
	sortBackupRows(filtered)
	return filtered, nil
}

func (s *BackupService) BackupGuest(ctx context.Context, options BackupCreateOptions) (output.BackupResult, error) {
	kind, err := parseBackupGuestKind(options.Kind)
	if err != nil {
		return output.BackupResult{}, err
	}
	if options.VMID <= 0 {
		return output.BackupResult{}, fmt.Errorf("invalid vmid %d", options.VMID)
	}
	if strings.TrimSpace(options.Storage) == "" {
		return output.BackupResult{}, fmt.Errorf("storage is required")
	}

	mode, err := ParseBackupMode(options.Mode)
	if err != nil {
		return output.BackupResult{}, err
	}
	compress, err := ParseBackupCompress(options.Compress)
	if err != nil {
		return output.BackupResult{}, err
	}
	protected, err := ParseBackupProtected(options.Protected)
	if err != nil {
		return output.BackupResult{}, err
	}

	guestSvc := NewVMService(s.backend, TaskRunner{}, nil, false)
	if kind == BackupKindLXC {
		guestSvc = NewLXCService(s.backend, TaskRunner{}, nil, false)
	}
	row, err := guestSvc.Get(ctx, options.VMID, options.Node)
	if err != nil {
		return output.BackupResult{}, err
	}

	task, err := s.backend.BackupGuest(ctx, row.Node, BackupOptions{
		Kind:          kind,
		VMID:          options.VMID,
		Storage:       options.Storage,
		Mode:          mode,
		Compress:      compress,
		NotesTemplate: options.NotesTemplate,
		BwLimit:       options.BwLimit,
		Protected:     protected,
	})
	if err != nil {
		return output.BackupResult{}, err
	}

	result := output.BackupResult{
		Kind:    kind,
		VMID:    uint64(options.VMID),
		Node:    row.Node,
		Storage: options.Storage,
		Mode:    mode,
	}
	if task != nil {
		result.Task = task.UPID()
	}

	if err := s.tasks.Handle(ctx, task); err != nil {
		return output.BackupResult{}, err
	}
	return result, nil
}

func ParseBackupKind(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", BackupKindAll:
		return BackupKindAll, nil
	case BackupKindVM:
		return BackupKindVM, nil
	case BackupKindLXC:
		return BackupKindLXC, nil
	default:
		return "", fmt.Errorf("invalid backup type %q, expected all, vm, or lxc", value)
	}
}

func ParseBackupMode(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", BackupModeSnapshot:
		return BackupModeSnapshot, nil
	case BackupModeSuspend:
		return BackupModeSuspend, nil
	case BackupModeStop:
		return BackupModeStop, nil
	default:
		return "", fmt.Errorf("invalid backup mode %q, expected snapshot, suspend, or stop", value)
	}
}

func ParseBackupCompress(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", BackupCompressZstd:
		return BackupCompressZstd, nil
	case BackupCompressLzo:
		return BackupCompressLzo, nil
	case BackupCompressGzip:
		return BackupCompressGzip, nil
	case BackupCompressNone:
		return BackupCompressNone, nil
	default:
		return "", fmt.Errorf("invalid backup compression %q, expected zstd, lzo, gzip, or none", value)
	}
}

func ParseBackupProtected(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "":
		return "", nil
	case "0":
		return "0", nil
	case "1":
		return "1", nil
	default:
		return "", fmt.Errorf("invalid protected value %q, expected 0 or 1", value)
	}
}

func parseBackupGuestKind(value string) (string, error) {
	kind, err := ParseBackupKind(value)
	if err != nil {
		return "", err
	}
	if kind == BackupKindAll {
		return "", fmt.Errorf("backup guest kind is required")
	}
	return kind, nil
}

func latestBackups(rows []output.BackupRow) []output.BackupRow {
	latest := make(map[string]output.BackupRow, len(rows))
	for _, row := range rows {
		key := row.Kind + "/" + strconv.FormatUint(row.VMID, 10)
		current, ok := latest[key]
		if !ok || row.CTime > current.CTime || (row.CTime == current.CTime && row.VolID > current.VolID) {
			latest[key] = row
		}
	}

	result := make([]output.BackupRow, 0, len(latest))
	for _, row := range latest {
		result = append(result, row)
	}
	return result
}

func sortBackupRows(rows []output.BackupRow) {
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Node != b.Node {
			return a.Node < b.Node
		}
		if a.Storage != b.Storage {
			return a.Storage < b.Storage
		}
		if a.VMID != b.VMID {
			return a.VMID < b.VMID
		}
		if a.CTime != b.CTime {
			return a.CTime > b.CTime
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.VolID < b.VolID
	})
}
