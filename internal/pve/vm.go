package pve

import (
	"context"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pvectl/internal/output"
)

type vmGuest struct {
	vm *proxmox.VirtualMachine
}

func (g vmGuest) Row() output.GuestRow {
	return vmRow(g.vm)
}

func (g vmGuest) Start(ctx context.Context) (Task, error) {
	task, err := g.vm.Start(ctx)
	return wrapTask(task), err
}

func (g vmGuest) Shutdown(ctx context.Context) (Task, error) {
	task, err := g.vm.Shutdown(ctx)
	return wrapTask(task), err
}

func (g vmGuest) Stop(ctx context.Context) (Task, error) {
	task, err := g.vm.Stop(ctx)
	return wrapTask(task), err
}

func (g vmGuest) Reboot(ctx context.Context) (Task, error) {
	task, err := g.vm.Reboot(ctx)
	return wrapTask(task), err
}

func (g vmGuest) Clone(ctx context.Context, options CloneOptions) (CloneResult, Task, error) {
	params := &proxmox.VirtualMachineCloneOptions{
		NewID:       options.NewID,
		Name:        options.Name,
		Target:      options.Target,
		Storage:     options.Storage,
		Full:        proxmox.IntOrBool(options.Full),
		Pool:        options.Pool,
		SnapName:    options.SnapName,
		Description: options.Description,
		Format:      options.Format,
	}
	newid, task, err := g.vm.Clone(ctx, params)
	if err != nil {
		return CloneResult{}, nil, err
	}
	wrapped := wrapTask(task)
	result := CloneResult{
		Kind:       "vm",
		SourceVMID: uint64(g.vm.VMID),
		NewVMID:    uint64(newid),
		SourceNode: g.vm.Node,
		TargetNode: options.Target,
		Name:       options.Name,
	}
	if wrapped != nil {
		result.Task = wrapped.UPID()
	}
	return result, wrapped, nil
}

func (g vmGuest) Config(ctx context.Context, values map[string]string) (Task, error) {
	options := make([]proxmox.VirtualMachineOption, 0, len(values))
	for key, value := range values {
		options = append(options, proxmox.VirtualMachineOption{Name: key, Value: value})
	}
	task, err := g.vm.Config(ctx, options...)
	return wrapTask(task), err
}

func (g vmGuest) Delete(ctx context.Context) (Task, error) {
	task, err := g.vm.Delete(ctx, nil)
	return wrapTask(task), err
}

func (g vmGuest) Migrate(ctx context.Context, options MigrateOptions) (Task, error) {
	task, err := g.vm.Migrate(ctx, &proxmox.VirtualMachineMigrateOptions{
		Target: options.Target,
		Online: proxmox.IntOrBool(options.Online),
	})
	return wrapTask(task), err
}

func (g vmGuest) Resize(ctx context.Context, disk, size string) (Task, error) {
	task, err := g.vm.ResizeDisk(ctx, disk, size)
	return wrapTask(task), err
}

func (g vmGuest) Snapshots(ctx context.Context) ([]output.SnapshotRow, error) {
	snapshots, err := g.vm.Snapshots(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]output.SnapshotRow, 0, len(snapshots))
	for _, snapshot := range snapshots {
		rows = append(rows, output.SnapshotRow{
			Kind:        "vm",
			VMID:        uint64(g.vm.VMID),
			Node:        g.vm.Node,
			Name:        snapshot.Name,
			Description: snapshot.Description,
			Parent:      snapshot.Parent,
			Snaptime:    snapshot.Snaptime,
			VMState:     snapshot.Vmstate,
			State:       snapshot.Snapstate,
		})
	}
	return rows, nil
}

func (g vmGuest) CreateSnapshot(ctx context.Context, name string) (Task, error) {
	task, err := g.vm.NewSnapshot(ctx, name)
	return wrapTask(task), err
}

func (g vmGuest) RollbackSnapshot(ctx context.Context, name string) (Task, error) {
	task, err := g.vm.Snapshot(name).Rollback(ctx)
	return wrapTask(task), err
}
