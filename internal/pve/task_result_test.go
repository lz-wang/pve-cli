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

// A nil task or an empty UPID can never be a success: the submission outcome
// is unconfirmed and the runner must say so instead of reporting succeeded.
func TestTaskRunnerNeverSucceedsOnUnconfirmableSubmission(t *testing.T) {
	result, err := (TaskRunner{}).Execute(context.Background(), nil)
	if err == nil || result.Status != "unknown" {
		t.Fatalf("nil task: %+v %v", result, err)
	}
	if errors.Is(err, ErrTaskTimeout) == false {
		t.Fatalf("nil task err = %v, want ErrTaskTimeout", err)
	}

	result, err = (TaskRunner{}).Execute(context.Background(), &fakeTask{})
	if err == nil || result.Status != "unknown" || result.Task != "" {
		t.Fatalf("empty upid: %+v %v", result, err)
	}
	if !errors.Is(err, ErrTaskTimeout) {
		t.Fatalf("empty upid err = %v, want ErrTaskTimeout", err)
	}
}
