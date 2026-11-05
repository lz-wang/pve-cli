package pve

import (
	"context"
	"errors"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"strings"
	"testing"
	"time"
)

type auditBackend struct {
	*fakeBackend
	configs    map[int]map[string]any
	failBackup bool
}

func (b *auditBackend) LXCs(_ context.Context, node string) ([]output.GuestRow, error) {
	rows := []output.GuestRow{}
	for _, g := range b.lxcs[node] {
		rows = append(rows, g.row)
	}
	return rows, nil
}
func (b *auditBackend) GuestConfig(_ context.Context, _ string, _ string, id int) (map[string]any, error) {
	return b.configs[id], nil
}
func (b *auditBackend) Backups(ctx context.Context, node, storage string) ([]output.BackupRow, error) {
	if b.failBackup {
		return nil, errors.New("inventory unavailable")
	}
	return b.fakeBackend.Backups(ctx, node, storage)
}
func newAuditBackend() *auditBackend {
	return &auditBackend{fakeBackend: &fakeBackend{
		nodes: []output.NodeRow{{Name: "n"}},
		lxcs: map[string]map[int]*fakeGuest{"n": {
			200: {row: output.GuestRow{Kind: "lxc", VMID: 200, Node: "n", Name: "files"}},
			201: {row: output.GuestRow{Kind: "lxc", VMID: 201, Node: "n", Name: "old"}},
			202: {row: output.GuestRow{Kind: "lxc", VMID: 202, Node: "n", Name: "missing"}},
			300: {row: output.GuestRow{Kind: "lxc", VMID: 300, Node: "n", Name: "template"}},
		}},
		storageRows: map[string][]output.StorageRow{"n": {{Storage: "backup", Content: "backup", Active: true, Enabled: true}}},
		backupRows: map[string]map[string][]output.BackupRow{"n": {"backup": {
			{Kind: "lxc", VMID: 200, VolID: "backup:backup/vzdump-lxc-200.tar.zst", CTime: 9900},
			{Kind: "lxc", VMID: 201, VolID: "backup:backup/vzdump-lxc-201.tar.zst", CTime: 100},
			{Kind: "vm", VMID: 202, VolID: "backup:backup/vzdump-qemu-202.vma.zst", CTime: 9900},
		}}},
	}, configs: map[int]map[string]any{200: {"mp0": "/data,mp=/data,backup=0", "hostpci0": "secret-device", "password": "never-output"}, 300: {"template": 1}}}
}
func TestBackupAuditCoverageExclusionsAndTemplates(t *testing.T) {
	b := newAuditBackend()
	r, err := AuditBackups(context.Background(), b, BackupAuditOptions{MaxAge: time.Hour, Now: time.Unix(10000, 0)})
	if err != nil || !r.Complete || len(r.Rows) != 3 {
		t.Fatalf("%+v %v", r, err)
	}
	want := []string{"recent_candidate", "stale", "missing"}
	for i, row := range r.Rows {
		if row.Status != want[i] || row.Association != "kind_vmid_candidate" || row.RestoreVerified != "unknown" || row.OffHostCopy != "unknown" {
			t.Fatalf("%+v", row)
		}
	}
	text := strings.Join(r.Rows[0].Findings, " ")
	if !strings.Contains(text, "outside archive") || !strings.Contains(text, "external device") || strings.Contains(text, "never-output") {
		t.Fatal(text)
	}
	if len(r.Rows[2].Archives) != 0 {
		t.Fatal("VM archive must not cover LXC")
	}
	r, err = AuditBackups(context.Background(), b, BackupAuditOptions{MaxAge: time.Hour, Now: time.Unix(10000, 0), IDs: []int{300}, IncludeTemplates: true})
	if err != nil || len(r.Rows) != 1 || r.Rows[0].VMID != 300 {
		t.Fatalf("%+v %v", r, err)
	}
}
func TestBackupAuditUnavailableInventoryIsUnknown(t *testing.T) {
	b := newAuditBackend()
	b.failBackup = true
	r, err := AuditBackups(context.Background(), b, BackupAuditOptions{MaxAge: time.Hour})
	if err != nil || r.Complete || len(r.Errors) == 0 {
		t.Fatalf("%+v %v", r, err)
	}
	for _, row := range r.Rows {
		if row.Status != "unknown" {
			t.Fatalf("%+v", row)
		}
	}
}
func TestBackupAuditFutureTimeAndMissingSelection(t *testing.T) {
	b := newAuditBackend()
	r, _ := AuditBackups(context.Background(), b, BackupAuditOptions{MaxAge: time.Hour, Now: time.Unix(100, 0), IDs: []int{200, 999}})
	if r.Complete || len(r.Rows) != 1 || r.Rows[0].Status != "unknown" {
		t.Fatalf("%+v", r)
	}
}

func TestBackupAuditPBSDoesNotReportMissing(t *testing.T) {
	b := newAuditBackend()
	b.storageRows["n"][0].Type = "pbs"
	r, err := AuditBackups(context.Background(), b, BackupAuditOptions{MaxAge: time.Hour})
	if err != nil || r.Complete || len(r.Errors) == 0 {
		t.Fatalf("%+v %v", r, err)
	}
	for _, row := range r.Rows {
		if row.Status != "unknown" {
			t.Fatalf("unsupported PBS reported as %s", row.Status)
		}
	}
}
