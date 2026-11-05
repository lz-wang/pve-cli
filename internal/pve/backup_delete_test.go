package pve

import (
	"context"
	"errors"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"net/http"
	"testing"
)

type deleteArchiveFake struct {
	*fakeBackend
	row       output.BackupRow
	calls     int
	reads     int
	remain    bool
	failRead  bool
	submitErr error
	task      Task
}

func (b *deleteArchiveFake) Backups(context.Context, string, string) ([]output.BackupRow, error) {
	b.reads++
	if b.failRead && b.calls > 0 {
		return nil, errors.New("unavailable")
	}
	if b.calls > 0 && !b.remain {
		return []output.BackupRow{}, nil
	}
	return []output.BackupRow{b.row}, nil
}
func (b *deleteArchiveFake) DeleteBackup(context.Context, string, string) (Task, error) {
	b.calls++
	return b.task, b.submitErr
}
func TestDeleteBackupArchiveOutcomes(t *testing.T) {
	row := output.BackupRow{VolID: "local:backup/vzdump-lxc-900-test.tar.zst", Kind: "lxc", VMID: 900, Size: 100, CTime: 200}
	for _, tc := range []struct {
		name   string
		mutate func(*deleteArchiveFake)
		status string
		calls  int
		err    bool
	}{
		{"synchronous verified", func(*deleteArchiveFake) {}, "succeeded", 1, false},
		{"protected", func(b *deleteArchiveFake) { b.row.Protected = "1" }, "failed", 0, true},
		{"changed", func(b *deleteArchiveFake) { b.row.CTime++ }, "failed", 0, true},
		{"remaining", func(b *deleteArchiveFake) { b.remain = true }, "unknown", 1, true},
		{"unreadable", func(b *deleteArchiveFake) { b.failRead = true }, "unknown", 1, true},
		{"transport", func(b *deleteArchiveFake) { b.submitErr = errors.New("transport") }, "unknown", 1, true},
		{"rejected", func(b *deleteArchiveFake) { b.submitErr = &APIError{HTTPStatus: 403, kind: ErrPermissionDenied} }, "failed", 1, true},
		{"async", func(b *deleteArchiveFake) { b.task = &fakeTask{upid: "UPID:test"} }, "submitted", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &deleteArchiveFake{fakeBackend: &fakeBackend{}, row: row}
			tc.mutate(b)
			r, err := DeleteBackupArchive(context.Background(), b, TaskRunner{}, "n", row)
			if r.Status != tc.status || b.calls != tc.calls || (err != nil) != tc.err {
				t.Fatalf("%+v %v calls=%d", r, err, b.calls)
			}
			if tc.name == "async" && r.Task == "" {
				t.Fatal("missing task")
			}
			if r.Status == "succeeded" && (r.Verified == nil || !*r.Verified) {
				t.Fatal("success without verification")
			}
		})
	}
	for _, bad := range []string{"/tmp/a.tar.zst", "local:backup/*", "local:backup/../vzdump-lxc-900-test.tar.zst", "local:images/vzdump-lxc-900-test.tar.zst", "local:backup/vzdump-lxc-900-test.tar.zst.notes", "pbs:backup/ct/900/2020"} {
		if _, _, err := validateDeletableArchive(bad); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
func TestDeleteBackupHTTP(t *testing.T) {
	archive := "local:backup/vzdump-lxc-900-test.tar.zst"
	for _, response := range []string{`{"data":null}`, `{"data":"UPID:test"}`} {
		calls := 0
		b := contractBackend(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Method != "DELETE" || r.URL.Path != "/nodes/n/storage/local/content/"+archive || r.URL.RawQuery != "" {
				t.Errorf("unexpected %s %s", r.Method, r.URL)
			}
			_, _ = w.Write([]byte(response))
		})
		task, err := b.DeleteBackup(context.Background(), "n", archive)
		if err != nil || calls != 1 {
			t.Fatalf("%v calls=%d", err, calls)
		}
		if response == `{"data":null}` && task != nil {
			t.Fatal("synchronous response treated as task")
		}
	}
}
