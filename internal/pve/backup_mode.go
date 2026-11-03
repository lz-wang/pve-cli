package pve

import (
	"context"
	"regexp"
	"strings"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

// BackupEffectiveModeSource identifies where the effective backup mode was
// determined from.
const BackupEffectiveModeSource = "task_log"

const BackupModeUnknown = "unknown"

// vzdump writes one of these markers when it starts a mode or falls back to
// another one. Only explicit markers count; a missing marker never implies
// the requested mode ran.
var backupModeMarkers = []*regexp.Regexp{
	regexp.MustCompile(`starting backup mode: (snapshot|suspend|stop)`),
	regexp.MustCompile(`backup mode: (snapshot|suspend|stop)`),
	regexp.MustCompile(`fall(?:ing)? back to '?(snapshot|suspend|stop)'? mode`),
	regexp.MustCompile(`fall back to '(snapshot|suspend|stop)'`),
}

// parseEffectiveBackupMode scans task log lines for explicit vzdump mode
// markers. The last marker wins because vzdump logs a new marker after a
// fallback. No marker means the mode cannot be determined reliably.
func parseEffectiveBackupMode(rows []output.TaskLogRow) (mode string, found bool) {
	for _, row := range rows {
		for _, marker := range backupModeMarkers {
			if match := marker.FindStringSubmatch(strings.ToLower(row.Text)); match != nil {
				mode = match[1]
				found = true
			}
		}
	}
	return mode, found
}

// taskLogReader is the minimal backend capability for reading finished task
// logs to determine the effective backup mode.
type taskLogReader interface {
	TaskLog(ctx context.Context, node, upid string, page TaskLogPage) ([]output.TaskLogRow, error)
}

// effectiveBackupMode reads the finished task's log and reports the mode it
// actually used. Any failure or ambiguity yields unknown; the backup result
// itself is unaffected.
func (s *BackupService) effectiveBackupMode(ctx context.Context, task Task) (mode, source string) {
	if task == nil {
		return BackupModeUnknown, ""
	}
	reader, ok := s.backend.(taskLogReader)
	if !ok {
		return BackupModeUnknown, ""
	}
	upid := task.UPID()
	info, err := ParseUPID(upid)
	if err != nil {
		return BackupModeUnknown, ""
	}
	var rows []output.TaskLogRow
	for start, pages := 0, 0; pages < taskLogMaxPages; start, pages = start+taskLogPageSize, pages+1 {
		page, err := reader.TaskLog(ctx, info.Node, upid, TaskLogPage{Start: start, Limit: taskLogPageSize})
		if err != nil {
			return BackupModeUnknown, ""
		}
		rows = append(rows, page...)
		if len(page) < taskLogPageSize {
			break
		}
	}
	if mode, found := parseEffectiveBackupMode(rows); found {
		return mode, BackupEffectiveModeSource
	}
	return BackupModeUnknown, ""
}
