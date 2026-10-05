package pve

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

// Bulk lifecycle action names.
const (
	BulkActionStart    = "start"
	BulkActionShutdown = "shutdown"
	BulkActionReboot   = "reboot"
	BulkActionStop     = "stop"
)

// Bulk result statuses.
const (
	BulkResultStatusOK    = "ok"
	BulkResultStatusError = "error"
)

// GuestSelector describes which guests a bulk operation targets. At least one
// constraining field (Node, Status, or Tags) must be set so a bare command
// never sweeps the whole cluster.
type GuestSelector struct {
	Node     string
	Type     GuestType
	Status   string
	Tags     []string
	TagMatch string
}

// Validate rejects selectors that would match the whole cluster by default.
func (sel GuestSelector) Validate() error {
	if sel.Node == "" && sel.Status == "" && len(sel.Tags) == 0 {
		return fmt.Errorf("bulk operations require at least one of --node, --status, or --tag to limit the scope")
	}
	return nil
}

// BulkService resolves guest selections for bulk operations.
type BulkService struct {
	backend GuestBackend
	tasks   TaskRunner
	logger  *slog.Logger
	verbose bool
}

func NewBulkService(backend GuestBackend, tasks TaskRunner, logger *slog.Logger, verbose bool) *BulkService {
	return &BulkService{backend: backend, tasks: tasks, logger: logger, verbose: verbose}
}

// Select resolves the selector to matching guest rows across all nodes.
// Selection is the input to mutations, so it fails closed: when any node
// cannot be queried, the selection is refused instead of proceeding with a
// partial view of the cluster.
func (s *BulkService) Select(ctx context.Context, selector GuestSelector) ([]output.GuestRow, error) {
	if err := selector.Validate(); err != nil {
		return nil, err
	}
	tagMatch, err := ParseTagMatch(selector.TagMatch)
	if err != nil {
		return nil, err
	}
	guestType := selector.Type
	if guestType == "" {
		guestType = GuestTypeAll
	}
	if _, err := ParseGuestListType(string(guestType)); err != nil {
		return nil, err
	}

	rows, err := NewGuestAggregateService(s.backend, s.logger, s.verbose).List(ctx, GuestListOptions{
		Node:            selector.Node,
		Type:            guestType,
		Status:          selector.Status,
		Tags:            selector.Tags,
		TagMatch:        tagMatch,
		RequireComplete: true,
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ParseBulkAction validates a bulk lifecycle action name.
func ParseBulkAction(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case BulkActionStart:
		return BulkActionStart, nil
	case BulkActionShutdown:
		return BulkActionShutdown, nil
	case BulkActionReboot:
		return BulkActionReboot, nil
	case BulkActionStop:
		return BulkActionStop, nil
	default:
		return "", fmt.Errorf("invalid bulk action %q, expected start, shutdown, reboot, or stop", value)
	}
}

// BulkExecuteOptions controls concurrent execution of a lifecycle action
// over an already-selected guest list.
type BulkExecuteOptions struct {
	Action      string
	Jobs        int
	Wait        bool
	WaitTimeout time.Duration
	ErrWriter   io.Writer
}

// syncWriter serializes progress writes. Every guest runs in its own
// goroutine and shares one ErrWriter, which io.Writer does not guarantee to
// be concurrency-safe (bytes.Buffer, commonly used in tests, is not).
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

// bulkGuestOutcome records what happened to one guest. Failures are kept per
// guest so one bad guest never aborts the whole run.
type bulkGuestOutcome struct {
	row      output.GuestRow
	taskUpid string
	err      error
}

// ExecuteRows runs the action over every selected guest with bounded
// concurrency. It never stops on the first failure; structured per-guest
// results are returned alongside an aggregated error so callers can print
// every outcome before failing the command.
func (s *BulkService) ExecuteRows(ctx context.Context, action string, rows []output.GuestRow, options BulkExecuteOptions) ([]output.BulkGuestResult, error) {
	action, err := ParseBulkAction(action)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no guests match the selection")
	}

	jobs := options.Jobs
	if jobs <= 0 {
		jobs = 2
	}

	// Concurrent TaskRunner handles share one progress writer; serialize it
	// so bulk progress stays race-free even for non-thread-safe writers.
	errWriter := options.ErrWriter
	if errWriter != nil {
		errWriter = &syncWriter{w: options.ErrWriter}
	}
	execOptions := options
	execOptions.ErrWriter = errWriter

	outcomes := make([]bulkGuestOutcome, len(rows))
	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	for i, row := range rows {
		wg.Add(1)
		go func(i int, row output.GuestRow) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			outcomes[i] = s.executeOne(ctx, action, row, execOptions)
		}(i, row)
	}
	wg.Wait()

	results := make([]output.BulkGuestResult, len(outcomes))
	failed := 0
	for i, outcome := range outcomes {
		result := output.BulkGuestResult{
			Kind:   outcome.row.Kind,
			VMID:   outcome.row.VMID,
			Node:   outcome.row.Node,
			Name:   outcome.row.Name,
			Action: action,
			Status: BulkResultStatusOK,
			Task:   outcome.taskUpid,
		}
		if outcome.err != nil {
			failed++
			result.Status = BulkResultStatusError
			result.Error = outcome.err.Error()
			s.progress(errWriter, "%s %d %s: error: %v\n", outcome.row.Kind, outcome.row.VMID, outcome.row.Name, outcome.err)
		} else {
			s.progress(errWriter, "%s %d %s: ok\n", outcome.row.Kind, outcome.row.VMID, outcome.row.Name)
		}
		results[i] = result
	}
	if failed > 0 {
		return results, fmt.Errorf("bulk %s completed with %d failure(s) out of %d guest(s)", action, failed, len(rows))
	}
	return results, nil
}

func (s *BulkService) executeOne(ctx context.Context, action string, row output.GuestRow, options BulkExecuteOptions) bulkGuestOutcome {
	vmid := int(row.VMID)
	guest, err := s.getOnNode(ctx, row.Kind, row.Node, vmid)
	if err != nil {
		return bulkGuestOutcome{row: row, err: err}
	}

	var task Task
	switch action {
	case BulkActionStart:
		task, err = guest.Start(ctx)
	case BulkActionShutdown:
		task, err = guest.Shutdown(ctx)
	case BulkActionReboot:
		task, err = guest.Reboot(ctx)
	case BulkActionStop:
		task, err = guest.Stop(ctx)
	}
	if err != nil {
		return bulkGuestOutcome{row: row, err: err}
	}

	runner := TaskRunner{
		Wait:        options.Wait,
		WaitTimeout: options.WaitTimeout,
		ErrWriter:   options.ErrWriter,
	}
	if err := runner.Handle(ctx, task); err != nil {
		return bulkGuestOutcome{row: row, taskUpid: taskUPID(task), err: err}
	}
	return bulkGuestOutcome{row: row, taskUpid: taskUPID(task)}
}

func (s *BulkService) getOnNode(ctx context.Context, kind, node string, vmid int) (Guest, error) {
	if kind == "vm" {
		return s.backend.VM(ctx, node, vmid)
	}
	return s.backend.LXC(ctx, node, vmid)
}

func taskUPID(task Task) string {
	if task == nil {
		return ""
	}
	return task.UPID()
}

func (s *BulkService) progress(w io.Writer, format string, args ...any) {
	if w != nil {
		fmt.Fprintf(w, format, args...)
	}
}
