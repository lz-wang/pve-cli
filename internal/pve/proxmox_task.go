package pve

import (
	"context"
	"fmt"
	"time"

	proxmox "github.com/luthermonson/go-proxmox"
)

type proxmoxTask struct {
	task *proxmox.Task
}

func wrapTask(task *proxmox.Task) Task {
	if task == nil {
		return nil
	}
	return proxmoxTask{task: task}
}

func (t proxmoxTask) UPID() string {
	return string(t.task.UPID)
}

func (t proxmoxTask) WaitFor(ctx context.Context, seconds int) error {
	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := t.task.Ping(waitCtx); err != nil {
			return err
		}
		switch t.task.Status {
		case "stopped":
			return nil
		case "running":
		default:
			return fmt.Errorf("unknown task status %q", t.task.Status)
		}
		select {
		case <-waitCtx.Done():
			return waitCtx.Err()
		case <-ticker.C:
		}
	}
}

func (t proxmoxTask) ExitStatus() string {
	return t.task.ExitStatus
}

func (t proxmoxTask) Failed() bool {
	return t.task.IsFailed
}
