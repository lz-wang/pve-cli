package pve

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lz-wang/pvectl/internal/output"
)

func TestParseUPID(t *testing.T) {
	info, err := ParseUPID("UPID:pve1:0000C8F0:00E199B5:6839F4A1:vzdump:100:root@pam:")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if info.Node != "pve1" || info.Type != "vzdump" || info.ID != "100" || info.User != "root@pam" {
		t.Fatalf("info = %#v", info)
	}
	if info.StartTime != 0x6839F4A1 {
		t.Fatalf("start time = %d", info.StartTime)
	}

	for _, upid := range []string{"", "not-an-upid", "UPID::0001:0002:6839F4A1:vzdump:100:root@pam"} {
		if _, err := ParseUPID(upid); err == nil {
			t.Fatalf("expected error for %q", upid)
		}
	}
}

func TestNormalizeTaskStatus(t *testing.T) {
	cases := []struct {
		status     string
		exitStatus string
		want       string
	}{
		{"running", "", TaskStatusRunning},
		{"stopped", "OK", TaskStatusOK},
		{"stopped", "interrupted by signal", TaskStatusError},
		{"stopped", "", TaskStatusUnknown},
		{"", "", TaskStatusUnknown},
	}
	for _, tc := range cases {
		if got := normalizeTaskStatus(tc.status, tc.exitStatus); got != tc.want {
			t.Fatalf("normalizeTaskStatus(%q, %q) = %q, want %q", tc.status, tc.exitStatus, got, tc.want)
		}
	}
}

func TestTaskServiceListAggregatesNodesWithPartialFailure(t *testing.T) {
	backend := &fakeTaskBackend{
		nodes: []output.NodeRow{{Name: "pve1"}, {Name: "pve2"}},
		taskRows: map[string][]output.TaskRow{
			"pve1": {{UPID: "UPID:pve1:1", Node: "pve1", Type: "vzdump", Status: "ok", StartTime: 100}},
			"pve2": {{UPID: "UPID:pve2:1", Node: "pve2", Type: "qmstart", Status: "running", StartTime: 200}},
		},
		taskErrs: map[string]error{"pve1": errors.New("forbidden")},
	}
	svc := NewTaskService(backend, nil, false)

	rows, err := svc.List(context.Background(), TaskListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Node != "pve2" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestTaskServiceListFailsWhenAllNodesFail(t *testing.T) {
	backend := &fakeTaskBackend{
		nodes:    []output.NodeRow{{Name: "pve1"}},
		taskErrs: map[string]error{"pve1": errors.New("forbidden")},
	}
	svc := NewTaskService(backend, nil, false)

	if _, err := svc.List(context.Background(), TaskListOptions{}); err == nil {
		t.Fatal("expected failure when all nodes fail")
	}
}

func TestTaskServiceListFiltersAndLimits(t *testing.T) {
	backend := &fakeTaskBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		taskRows: map[string][]output.TaskRow{
			"pve1": {
				{UPID: "UPID:pve1:1", Node: "pve1", Type: "vzdump", Status: "ok", StartTime: 100},
				{UPID: "UPID:pve1:2", Node: "pve1", Type: "vzdump", Status: "error", StartTime: 300},
				{UPID: "UPID:pve1:3", Node: "pve1", Type: "qmstart", Status: "running", StartTime: 200},
			},
		},
	}
	svc := NewTaskService(backend, nil, false)

	rows, err := svc.List(context.Background(), TaskListOptions{Type: "vzdump"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 || rows[0].UPID != "UPID:pve1:2" {
		t.Fatalf("type filter rows = %#v", rows)
	}

	rows, err = svc.List(context.Background(), TaskListOptions{Status: "running"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != "running" {
		t.Fatalf("status filter rows = %#v", rows)
	}

	rows, err = svc.List(context.Background(), TaskListOptions{Limit: 1})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].UPID != "UPID:pve1:2" {
		t.Fatalf("limit rows = %#v", rows)
	}

	if _, err := svc.List(context.Background(), TaskListOptions{Status: "bogus"}); err == nil {
		t.Fatal("expected invalid status error")
	}
}

func TestTaskServiceGetUsesNodeFromUPID(t *testing.T) {
	backend := &fakeTaskBackend{
		taskByName: map[string]output.TaskRow{
			"UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam": {
				UPID: "UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam", Node: "pve1", Status: "ok",
			},
		},
	}
	svc := NewTaskService(backend, nil, false)

	row, err := svc.Get(context.Background(), "UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.Node != "pve1" {
		t.Fatalf("row = %#v", row)
	}

	if _, err := svc.Get(context.Background(), "bogus"); err == nil {
		t.Fatal("expected invalid upid error")
	}
}

func TestTaskServiceLogPaginatesAndTails(t *testing.T) {
	backend := &fakeTaskBackend{
		logPages: map[string][][]output.TaskLogRow{
			"UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam": {
				pageLines(1, taskLogPageSize),
				pageLines(taskLogPageSize+1, 20),
			},
		},
	}
	svc := NewTaskService(backend, nil, false)

	rows, err := svc.Log(context.Background(), "UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam", TaskLogOptions{})
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(rows) != taskLogPageSize+20 {
		t.Fatalf("rows = %d", len(rows))
	}

	rows, err = svc.Log(context.Background(), "UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam", TaskLogOptions{Tail: 5})
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("tail rows = %d", len(rows))
	}
	if rows[0].Line != taskLogPageSize+16 {
		t.Fatalf("first tail line = %d", rows[0].Line)
	}
}

func TestTaskServiceWaitReportsFailureWithRow(t *testing.T) {
	backend := &fakeTaskBackend{
		taskHandle: &fakeTask{upid: "UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam", failed: true, exitStatus: "ERROR", waited: false},
		taskByName: map[string]output.TaskRow{
			"UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam": {
				UPID: "UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam", Node: "pve1",
				Status: TaskStatusError, ExitStatus: "ERROR",
			},
		},
	}
	svc := NewTaskService(backend, nil, false)

	row, err := svc.Wait(context.Background(), "UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam", time.Second)
	if err == nil {
		t.Fatal("expected task failure")
	}
	if row.Status != TaskStatusError {
		t.Fatalf("row = %#v", row)
	}
	if !backend.taskHandle.waited {
		t.Fatal("expected task to be waited")
	}
}

func TestTaskServiceWaitSuccess(t *testing.T) {
	backend := &fakeTaskBackend{
		taskHandle: &fakeTask{upid: "UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam"},
		taskByName: map[string]output.TaskRow{
			"UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam": {
				UPID: "UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam", Node: "pve1",
				Status: TaskStatusOK, ExitStatus: "OK",
			},
		},
	}
	svc := NewTaskService(backend, nil, false)

	row, err := svc.Wait(context.Background(), "UPID:pve1:0001:0000:6839F4A1:vzdump:100:root@pam", time.Second)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if row.Status != TaskStatusOK {
		t.Fatalf("row = %#v", row)
	}
}

type fakeTaskBackend struct {
	nodes      []output.NodeRow
	taskRows   map[string][]output.TaskRow
	taskErrs   map[string]error
	taskByName map[string]output.TaskRow
	logPages   map[string][][]output.TaskLogRow
	taskHandle *fakeTask
	taskCalls  int
	nodeCalls  int
}

func (b *fakeTaskBackend) Nodes(context.Context) ([]output.NodeRow, error) {
	b.nodeCalls++
	return b.nodes, nil
}

func (b *fakeTaskBackend) Node(context.Context, string) (output.NodeDetail, error) {
	return output.NodeDetail{}, ErrNotFound
}

func (b *fakeTaskBackend) Tasks(_ context.Context, node string, _ TaskListBackendOptions) ([]output.TaskRow, error) {
	if err := b.taskErrs[node]; err != nil {
		return nil, err
	}
	return b.taskRows[node], nil
}

func (b *fakeTaskBackend) Task(_ context.Context, _, upid string) (output.TaskRow, error) {
	b.taskCalls++
	row, ok := b.taskByName[upid]
	if !ok {
		return output.TaskRow{}, ErrNotFound
	}
	return row, nil
}

func (b *fakeTaskBackend) TaskLog(_ context.Context, _, upid string, page TaskLogPage) ([]output.TaskLogRow, error) {
	pages := b.logPages[upid]
	idx := page.Start / taskLogPageSize
	if idx >= len(pages) {
		return nil, nil
	}
	return pages[idx], nil
}

func (b *fakeTaskBackend) TaskHandle(string) (Task, error) {
	if b.taskHandle == nil {
		return nil, errors.New("no task handle")
	}
	return b.taskHandle, nil
}

func pageLines(start, count int) []output.TaskLogRow {
	rows := make([]output.TaskLogRow, 0, count)
	for i := 0; i < count; i++ {
		line := start + i
		rows = append(rows, output.TaskLogRow{Line: line, Text: fmt.Sprintf("line %d", line)})
	}
	return rows
}
