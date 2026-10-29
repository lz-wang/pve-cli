package pve

import (
	"context"
	"errors"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"testing"
)

type cancelBackend struct {
	TaskBackend
	row   output.TaskRow
	calls int
}

func (b *cancelBackend) Task(context.Context, string, string) (output.TaskRow, error) {
	return b.row, nil
}
func (b *cancelBackend) CancelTask(context.Context, string, string) error { b.calls++; return nil }
func TestCancelRunningOnly(t *testing.T) {
	b := &cancelBackend{row: output.TaskRow{Status: TaskStatusRunning}}
	s := NewTaskService(b, nil, false)
	upid := "UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam:"
	r, e := s.Cancel(context.Background(), upid)
	if e != nil || r.Status != "cancel_requested" || b.calls != 1 {
		t.Fatalf("%+v %v", r, e)
	}
	b.row.Status = TaskStatusOK
	if _, e = s.Cancel(context.Background(), upid); !errors.Is(e, ErrConflict) || b.calls != 1 {
		t.Fatal(e)
	}
}
