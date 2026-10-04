package pve

import (
	"context"
	"errors"

	"github.com/lz-wang/pvectl/internal/output"
)

var ErrNotFound = errors.New("not found")

type Guest interface {
	Row() output.GuestRow
	Start(ctx context.Context) (Task, error)
	Shutdown(ctx context.Context) (Task, error)
	Stop(ctx context.Context) (Task, error)
	Reboot(ctx context.Context) (Task, error)
	Clone(ctx context.Context, options CloneOptions) (CloneResult, Task, error)
	Config(ctx context.Context, values map[string]string) (Task, error)
	Delete(ctx context.Context) (Task, error)
	Migrate(ctx context.Context, options MigrateOptions) (Task, error)
	Resize(ctx context.Context, disk, size string) (Task, error)
	Snapshots(ctx context.Context) ([]output.SnapshotRow, error)
	CreateSnapshot(ctx context.Context, name string) (Task, error)
	RollbackSnapshot(ctx context.Context, name string) (Task, error)
	DeleteSnapshot(ctx context.Context, name string) (Task, error)
}

type Task interface {
	UPID() string
	WaitFor(ctx context.Context, seconds int) error
	ExitStatus() string
	Failed() bool
}

type CloneOptions struct {
	NewID       int
	Name        string
	Hostname    string
	Target      string
	Storage     string
	Full        bool
	Pool        string
	SnapName    string
	Description string
	Format      string
}

type CloneResult = output.CloneResult

type MigrateOptions struct {
	Target string
	Online bool
}

type BackupOptions struct {
	Kind          string
	VMID          int
	Storage       string
	Mode          string
	Compress      string
	NotesTemplate string
	BwLimit       uint
	Protected     string
}
