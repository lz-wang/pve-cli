package pve

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/lz-wang/pvectl/internal/output"
)

// Task status values used in TaskRow.Status.
const (
	TaskStatusRunning = "running"
	TaskStatusOK      = "ok"
	TaskStatusError   = "error"
	TaskStatusUnknown = "unknown"
)

// TaskBackend covers task inspection plus node discovery.
type TaskBackend interface {
	NodeBackend
	Tasks(ctx context.Context, node string, options TaskListBackendOptions) ([]output.TaskRow, error)
	Task(ctx context.Context, node, upid string) (output.TaskRow, error)
	TaskLog(ctx context.Context, node, upid string, page TaskLogPage) ([]output.TaskLogRow, error)
	TaskHandle(upid string) (Task, error)
}

// TaskListBackendOptions maps list filters that can be pushed down to the PVE
// tasks index API.
type TaskListBackendOptions struct {
	TypeFilter string
}

// TaskLogPage is one page of a task log request against the PVE log API.
type TaskLogPage struct {
	Start int
	Limit int
}

const taskLogPageSize = 500

func (b *ProxmoxBackend) Tasks(ctx context.Context, nodeName string, options TaskListBackendOptions) ([]output.TaskRow, error) {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return nil, fmt.Errorf("node is required")
	}

	node, err := b.client.Node(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	opts := &proxmox.NodeTasksOptions{}
	if strings.TrimSpace(options.TypeFilter) != "" {
		opts.TypeFilter = options.TypeFilter
	}
	tasks, err := node.Tasks(ctx, opts)
	if err != nil {
		return nil, err
	}

	rows := make([]output.TaskRow, 0, len(tasks))
	for _, task := range tasks {
		rows = append(rows, taskRow(task))
	}
	return rows, nil
}

func (b *ProxmoxBackend) Task(ctx context.Context, nodeName, upid string) (output.TaskRow, error) {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return output.TaskRow{}, fmt.Errorf("node is required")
	}
	task, err := b.taskHandle(upid)
	if err != nil {
		return output.TaskRow{}, err
	}
	if err := task.Ping(ctx); err != nil {
		return output.TaskRow{}, err
	}
	return taskRow(task), nil
}

func (b *ProxmoxBackend) TaskLog(ctx context.Context, nodeName, upid string, page TaskLogPage) ([]output.TaskLogRow, error) {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return nil, fmt.Errorf("node is required")
	}
	task, err := b.taskHandle(upid)
	if err != nil {
		return nil, err
	}
	limit := page.Limit
	if limit <= 0 {
		limit = taskLogPageSize
	}
	logs, err := task.Log(ctx, page.Start, limit)
	if err != nil {
		return nil, err
	}

	lines := make([]int, 0, len(logs))
	for line := range logs {
		lines = append(lines, line)
	}
	sortInts(lines)

	rows := make([]output.TaskLogRow, 0, len(logs))
	for _, line := range lines {
		rows = append(rows, output.TaskLogRow{Line: line, Text: logs[line]})
	}
	return rows, nil
}

func (b *ProxmoxBackend) TaskHandle(upid string) (Task, error) {
	task, err := b.taskHandle(upid)
	if err != nil {
		return nil, err
	}
	return wrapTask(task), nil
}

func (b *ProxmoxBackend) taskHandle(upid string) (*proxmox.Task, error) {
	upid = strings.TrimSpace(upid)
	if upid == "" {
		return nil, fmt.Errorf("upid is required")
	}
	info, err := ParseUPID(upid)
	if err != nil {
		return nil, err
	}
	task := proxmox.NewTask(proxmox.UPID(info.UPID), b.client)
	if task == nil {
		return nil, fmt.Errorf("invalid upid %q", upid)
	}
	return task, nil
}

func taskRow(task *proxmox.Task) output.TaskRow {
	if task == nil {
		return output.TaskRow{}
	}
	return output.TaskRow{
		UPID:       string(task.UPID),
		Node:       task.Node,
		Type:       task.Type,
		ID:         task.ID,
		User:       task.User,
		Status:     normalizeTaskStatus(task.Status, task.ExitStatus),
		ExitStatus: task.ExitStatus,
		StartTime:  unixTime(task.StartTime),
		EndTime:    unixTime(task.EndTime),
	}
}

// normalizeTaskStatus maps PVE task status fields onto the small vocabulary
// used by TaskRow.Status: running, ok, error, unknown.
func normalizeTaskStatus(status, exitStatus string) string {
	switch strings.TrimSpace(status) {
	case TaskStatusRunning:
		return TaskStatusRunning
	case TaskStatusUnknown, "":
		return TaskStatusUnknown
	case "stopped":
		switch strings.TrimSpace(exitStatus) {
		case "":
			return TaskStatusUnknown
		case "OK":
			return TaskStatusOK
		default:
			return TaskStatusError
		}
	default:
		return strings.TrimSpace(status)
	}
}

func unixTime(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.Unix()
}

func sortInts(values []int) {
	sort.Ints(values)
}
