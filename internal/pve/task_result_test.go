package pve

import (
	"context"
	"errors"
	"testing"
)

func TestTaskUnknownPreservesUPID(t *testing.T) {
	task := &fakeTask{upid: "recoverable", waitErr: context.DeadlineExceeded}
	result, err := (TaskRunner{Wait: true}).Execute(context.Background(), task)
	if !errors.Is(err, ErrTaskTimeout) || result.Task != "recoverable" || result.Status != "unknown" {
		t.Fatalf("%+v %v", result, err)
	}
	result, err = (TaskRunner{}).Execute(context.Background(), task)
	if err != nil || result.Status != "submitted" {
		t.Fatalf("%+v %v", result, err)
	}
}
