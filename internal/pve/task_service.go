package pve

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

// TaskService implements task inspection on top of TaskBackend.
type TaskService struct {
	backend TaskBackend
	logger  *slog.Logger
	verbose bool
}

func NewTaskService(backend TaskBackend, logger *slog.Logger, verbose bool) *TaskService {
	return &TaskService{backend: backend, logger: logger, verbose: verbose}
}

// TaskListOptions filters aggregated task lists.
type TaskListOptions struct {
	Node   string
	Type   string
	Status string
	Limit  int
}

// TaskLogOptions controls log retrieval. Tail keeps only the last N lines.
type TaskLogOptions struct {
	Tail int
}

// ParseTaskStatus validates the --status filter vocabulary.
func ParseTaskStatus(value string) (string, error) {
	status := strings.ToLower(strings.TrimSpace(value))
	switch status {
	case "":
		return "", nil
	case TaskStatusRunning, TaskStatusOK, TaskStatusError:
		return status, nil
	default:
		return "", fmt.Errorf("invalid task status %q, expected running, ok, or error", value)
	}
}

// List returns task rows. Without a node it aggregates across all nodes and
// tolerates per-node failures as long as at least one node succeeds.
func (s *TaskService) List(ctx context.Context, options TaskListOptions) ([]output.TaskRow, error) {
	status, err := ParseTaskStatus(options.Status)
	if err != nil {
		return nil, err
	}

	backendOpts := TaskListBackendOptions{TypeFilter: strings.TrimSpace(options.Type)}

	var rows []output.TaskRow
	if node := strings.TrimSpace(options.Node); node != "" {
		rows, err = s.backend.Tasks(ctx, node, backendOpts)
		if err != nil {
			return nil, err
		}
	} else {
		nodes, err := s.backend.Nodes(ctx)
		if err != nil {
			return nil, err
		}

		successes := 0
		var firstErr error
		for _, nodeRow := range nodes {
			if nodeRow.Name == "" {
				continue
			}
			nodeRows, err := s.backend.Tasks(ctx, nodeRow.Name, backendOpts)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				s.debug("skip node", "node", nodeRow.Name, "error", err)
				continue
			}
			successes++
			rows = append(rows, nodeRows...)
		}
		if successes == 0 && firstErr != nil {
			return nil, fmt.Errorf("list tasks: no nodes could be queried: %w", firstErr)
		}
	}

	rows = filterTaskRows(rows, status, strings.ToLower(strings.TrimSpace(options.Type)))
	sortTaskRows(rows)
	if options.Limit > 0 && len(rows) > options.Limit {
		rows = rows[:options.Limit]
	}
	return rows, nil
}

// Get returns the current status of a single task identified by UPID.
func (s *TaskService) Get(ctx context.Context, upid string) (output.TaskRow, error) {
	info, err := ParseUPID(upid)
	if err != nil {
		return output.TaskRow{}, err
	}
	return s.backend.Task(ctx, info.Node, info.UPID)
}

// Log returns task log lines, optionally limited to the last Tail lines.
func (s *TaskService) Log(ctx context.Context, upid string, options TaskLogOptions) ([]output.TaskLogRow, error) {
	info, err := ParseUPID(upid)
	if err != nil {
		return nil, err
	}

	var rows []output.TaskLogRow
	for start, pages := 0, 0; pages < taskLogMaxPages; start, pages = start+taskLogPageSize, pages+1 {
		page, err := s.backend.TaskLog(ctx, info.Node, info.UPID, TaskLogPage{Start: start, Limit: taskLogPageSize})
		if err != nil {
			return nil, err
		}
		rows = append(rows, page...)
		if len(page) < taskLogPageSize {
			break
		}
	}

	if options.Tail > 0 && len(rows) > options.Tail {
		rows = rows[len(rows)-options.Tail:]
	}
	return rows, nil
}

// Wait blocks until the task completes and returns its final row. A failed
// task yields both the final row and a non-nil error so callers can still
// print the result before failing the command.
func (s *TaskService) Wait(ctx context.Context, upid string, timeout time.Duration) (output.TaskRow, error) {
	info, err := ParseUPID(upid)
	if err != nil {
		return output.TaskRow{}, err
	}

	handle, err := s.backend.TaskHandle(info.UPID)
	if err != nil {
		return output.TaskRow{}, err
	}

	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	seconds := int(math.Ceil(timeout.Seconds()))
	if seconds <= 0 {
		seconds = 300
	}

	if err := handle.WaitFor(ctx, seconds); err != nil {
		return output.TaskRow{}, fmt.Errorf("task %s wait failed: %w", info.UPID, err)
	}

	row, rowErr := s.backend.Task(ctx, info.Node, info.UPID)
	if rowErr != nil {
		row = output.TaskRow{
			UPID:       info.UPID,
			Node:       info.Node,
			Type:       info.Type,
			ID:         info.ID,
			User:       info.User,
			Status:     TaskStatusUnknown,
			ExitStatus: handle.ExitStatus(),
		}
	}
	if handle.Failed() {
		return row, fmt.Errorf("task %s failed: %s", info.UPID, exitStatusOrUnknown(handle.ExitStatus()))
	}
	return row, nil
}

const taskLogMaxPages = 200

func filterTaskRows(rows []output.TaskRow, status, taskType string) []output.TaskRow {
	status = strings.ToLower(strings.TrimSpace(status))
	taskType = strings.ToLower(strings.TrimSpace(taskType))
	if status == "" && taskType == "" {
		return rows
	}

	out := rows[:0]
	for _, row := range rows {
		if status != "" && strings.ToLower(strings.TrimSpace(row.Status)) != status {
			continue
		}
		if taskType != "" && strings.ToLower(strings.TrimSpace(row.Type)) != taskType {
			continue
		}
		out = append(out, row)
	}
	return out
}

func sortTaskRows(rows []output.TaskRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].StartTime != rows[j].StartTime {
			return rows[i].StartTime > rows[j].StartTime
		}
		return rows[i].UPID > rows[j].UPID
	})
}

func exitStatusOrUnknown(exitStatus string) string {
	if strings.TrimSpace(exitStatus) == "" {
		return "unknown"
	}
	return exitStatus
}

func (s *TaskService) debug(msg string, args ...any) {
	if s.verbose && s.logger != nil {
		s.logger.Debug(msg, args...)
	}
}
