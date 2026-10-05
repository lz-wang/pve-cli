package pve

import (
	"context"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

type lxcGuest struct {
	ct *proxmox.Container
}

func (g lxcGuest) Row() output.GuestRow {
	return lxcRow(g.ct)
}

func (g lxcGuest) Start(ctx context.Context) (Task, error) {
	task, err := g.ct.Start(ctx)
	return wrapTask(task), err
}

func (g lxcGuest) Shutdown(ctx context.Context) (Task, error) {
	task, err := g.ct.Shutdown(ctx, false, 0)
	return wrapTask(task), err
}

func (g lxcGuest) Stop(ctx context.Context) (Task, error) {
	task, err := g.ct.Stop(ctx)
	return wrapTask(task), err
}

func (g lxcGuest) Reboot(ctx context.Context) (Task, error) {
	task, err := g.ct.Reboot(ctx)
	return wrapTask(task), err
}

func (g lxcGuest) Clone(ctx context.Context, options CloneOptions) (CloneResult, Task, error) {
	params := &proxmox.ContainerCloneOptions{
		NewID:       options.NewID,
		Hostname:    options.Hostname,
		Target:      options.Target,
		Storage:     options.Storage,
		Full:        proxmox.IntOrBool(options.Full),
		Pool:        options.Pool,
		SnapName:    options.SnapName,
		Description: options.Description,
	}
	newid, task, err := g.ct.Clone(ctx, params)
	if err != nil {
		return CloneResult{}, nil, err
	}
	wrapped := wrapTask(task)
	result := CloneResult{
		Kind:       "lxc",
		SourceVMID: uint64(g.ct.VMID),
		NewVMID:    uint64(newid),
		SourceNode: g.ct.Node,
		TargetNode: options.Target,
		Name:       options.Hostname,
	}
	if wrapped != nil {
		result.Task = wrapped.UPID()
	}
	return result, wrapped, nil
}

func (g lxcGuest) Config(ctx context.Context, values map[string]string) (Task, error) {
	options := make([]proxmox.ContainerOption, 0, len(values))
	for key, value := range values {
		options = append(options, proxmox.ContainerOption{Name: key, Value: value})
	}
	task, err := g.ct.Config(ctx, options...)
	return wrapTask(task), err
}

func (g lxcGuest) Delete(ctx context.Context) (Task, error) {
	task, err := g.ct.Delete(ctx, nil)
	return wrapTask(task), err
}

func (g lxcGuest) Migrate(ctx context.Context, options MigrateOptions) (Task, error) {
	task, err := g.ct.Migrate(ctx, &proxmox.ContainerMigrateOptions{
		Target: options.Target,
		Online: proxmox.IntOrBool(options.Online),
	})
	return wrapTask(task), err
}

func (g lxcGuest) Resize(ctx context.Context, disk, size string) (Task, error) {
	task, err := g.ct.Resize(ctx, disk, size)
	return wrapTask(task), err
}

func (g lxcGuest) Snapshots(ctx context.Context) ([]output.SnapshotRow, error) {
	snapshots, err := g.ct.Snapshots(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]output.SnapshotRow, 0, len(snapshots))
	for _, snapshot := range snapshots {
		rows = append(rows, output.SnapshotRow{
			Kind:        "lxc",
			VMID:        uint64(g.ct.VMID),
			Node:        g.ct.Node,
			Name:        snapshot.Name,
			Description: snapshot.Description,
			Parent:      snapshot.Parent,
			Snaptime:    snapshot.SnapshotCreationTime,
		})
	}
	return rows, nil
}

func (g lxcGuest) CreateSnapshot(ctx context.Context, name string) (Task, error) {
	task, err := g.ct.NewSnapshot(ctx, name)
	return wrapTask(task), err
}

func (g lxcGuest) RollbackSnapshot(ctx context.Context, name string) (Task, error) {
	task, err := g.ct.Snapshot(name).Rollback(ctx, false)
	return wrapTask(task), err
}

func (g lxcGuest) DeleteSnapshot(ctx context.Context, name string) (Task, error) {
	task, err := g.ct.Snapshot(name).Delete(ctx)
	return wrapTask(task), err
}
