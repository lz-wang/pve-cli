package pve

import (
	"context"
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"io"
	"math"
	"time"
)

type TaskRunner struct {
	Wait         bool
	WaitTimeout  time.Duration
	ErrWriter    io.Writer
	ResultWriter io.Writer
	Format       string
	Resource     string
	VMID         int
	Node         string
}

// Execute always returns the submitted UPID, including on timeout/interruption.
func (r TaskRunner) Execute(ctx context.Context, task Task) (output.OperationResult, error) {
	result := output.OperationResult{Resource: r.Resource, VMID: r.VMID, Node: r.Node, Status: "succeeded"}
	if task == nil {
		return result, nil
	}
	result.Task = task.UPID()
	result.Status = "submitted"
	if r.ErrWriter != nil {
		if _, err := fmt.Fprintf(r.ErrWriter, "task: %s\n", task.UPID()); err != nil {
			return result, err
		}
	}
	if !r.Wait {
		return result, nil
	}
	timeout := r.WaitTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if r.ErrWriter != nil {
		if _, err := fmt.Fprintf(r.ErrWriter, "waiting for task: %s\n", task.UPID()); err != nil {
			return result, err
		}
	}
	if err := task.WaitFor(waitCtx, int(math.Ceil(timeout.Seconds()))); err != nil {
		result.Status = "unknown"
		result.Error = "task wait failed; continue with task get/log/wait"
		return result, fmt.Errorf("%w: task %s wait failed: %w", ErrTaskTimeout, task.UPID(), err)
	}
	if task.Failed() {
		result.Status = "failed"
		result.Error = exitStatusOrUnknown(task.ExitStatus())
		return result, fmt.Errorf("%w: task %s failed: %s", ErrTaskFailed, task.UPID(), result.Error)
	}
	result.Status = "succeeded"
	if r.ErrWriter != nil {
		if _, err := fmt.Fprintf(r.ErrWriter, "task completed: %s\n", task.UPID()); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (r TaskRunner) Handle(ctx context.Context, task Task) error {
	result, err := r.Execute(ctx, task)
	if r.ResultWriter != nil {
		if e := output.WriteOperation(r.ResultWriter, r.Format, result); e != nil {
			return e
		}
	}
	return err
}
