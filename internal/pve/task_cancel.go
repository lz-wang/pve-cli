package pve

import (
	"context"
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"net/url"
)

// TaskCanceller requests cancellation without claiming that termination completed.
type TaskCanceller interface {
	CancelTask(context.Context, string, string) error
}

func (b *ProxmoxBackend) CancelTask(ctx context.Context, node, upid string) error {
	return ClassifyError(b.client.Delete(ctx, "/nodes/"+url.PathEscape(node)+"/tasks/"+url.PathEscape(upid), nil))
}
func (s *TaskService) Cancel(ctx context.Context, upid string) (output.OperationResult, error) {
	info, err := ParseUPID(upid)
	if err != nil {
		return output.OperationResult{}, err
	}
	row, err := s.Get(ctx, upid)
	if err != nil {
		return output.OperationResult{}, ClassifyError(err)
	}
	result := output.OperationResult{Resource: "task", Node: info.Node, Task: info.UPID, Status: "unknown"}
	if row.Status != TaskStatusRunning {
		return result, fmt.Errorf("%w: task is not running", ErrConflict)
	}
	backend, ok := s.backend.(TaskCanceller)
	if !ok {
		return result, fmt.Errorf("%w: task cancellation unavailable", ErrUnavailable)
	}
	if err = backend.CancelTask(ctx, info.Node, info.UPID); err != nil {
		return result, ClassifyError(err)
	}
	result.Status = "cancel_requested"
	return result, nil
}
