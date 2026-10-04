package pve

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lz-wang/pvectl/internal/output"
)

func parseCheckVMID(resource string) (uint64, error) {
	var vmid uint64
	if _, err := fmt.Sscanf(resource, "vm %d", &vmid); err != nil {
		return 0, err
	}
	return vmid, nil
}

func TestCheckServiceNodeAndStorageChecks(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{
			{Name: "pve1", Status: "online"},
			{Name: "pve2", Status: ""},
		},
		storageRows: map[string][]output.StorageRow{
			"pve1": {
				{Node: "pve1", Storage: "local", Active: true, Enabled: true, UsedFraction: 0.38},
				{Node: "pve1", Storage: "backup", Active: true, Enabled: true, UsedFraction: 0.87},
			},
			"pve2": {
				{Node: "pve2", Storage: "broken", Active: false, Enabled: true},
				{Node: "pve2", Storage: "off", Active: true, Enabled: false},
			},
		},
	}
	svc := NewCheckService(backend)

	result, err := svc.Run(context.Background(), CheckOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !result.Failed {
		t.Fatal("expected failure rows")
	}
	if !result.Warned {
		t.Fatal("expected warning rows")
	}

	byKey := map[string]output.CheckRow{}
	for _, row := range result.Rows {
		byKey[row.Check+"/"+row.Resource] = row
	}
	if row := byKey["node-online/pve1"]; row.Status != output.DoctorStatusOK {
		t.Fatalf("pve1 row = %#v", row)
	}
	if row := byKey["node-online/pve2"]; row.Status != output.DoctorStatusFail {
		t.Fatalf("pve2 row = %#v", row)
	}
	if row := byKey["storage-usage/pve1/backup"]; row.Status != output.DoctorStatusWarn {
		t.Fatalf("backup usage row = %#v", row)
	}
	if row := byKey["storage/pve2/broken"]; row.Status != output.DoctorStatusFail {
		t.Fatalf("broken row = %#v", row)
	}
	if row := byKey["storage/pve2/off"]; row.Status != output.DoctorStatusWarn {
		t.Fatalf("off row = %#v", row)
	}
}

func TestCheckServiceCustomThresholds(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1", Status: "online"}},
		storageRows: map[string][]output.StorageRow{
			"pve1": {{Node: "pve1", Storage: "data", Active: true, Enabled: true, UsedFraction: 0.90}},
		},
	}
	svc := NewCheckService(backend)

	result, err := svc.Run(context.Background(), CheckOptions{StorageWarn: 80, StorageFail: 99})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var found bool
	for _, row := range result.Rows {
		if row.Check == "storage-usage" && row.Resource == "pve1/data" {
			found = true
			if row.Status != output.DoctorStatusWarn {
				t.Fatalf("row = %#v", row)
			}
		}
	}
	if !found {
		t.Fatalf("no usage row: %#v", result.Rows)
	}

	result, err = svc.Run(context.Background(), CheckOptions{StorageWarn: 80, StorageFail: 89})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, row := range result.Rows {
		if row.Check == "storage-usage" && row.Status != output.DoctorStatusFail {
			t.Fatalf("expected fail at 90%% with fail threshold 89: %#v", row)
		}
	}
}

func TestCheckServiceStorageListingFailureWarns(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1", Status: "online"}},
		vmRows: map[string][]output.GuestRow{
			"pve1": {{Kind: "vm", VMID: 100, Node: "pve1", Tags: "backup"}},
		},
		lxcs:        map[string]map[int]*fakeGuest{},
		lxcRows:     map[string][]output.GuestRow{},
		storageErrs: map[string]error{"pve1": errors.New("timeout")},
	}
	svc := NewCheckService(backend)

	result, err := svc.Run(context.Background(), CheckOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var found bool
	for _, row := range result.Rows {
		if row.Check == "storage" && row.Resource == "pve1" && row.Status == output.DoctorStatusWarn {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected storage warning row: %#v", result.Rows)
	}
}

func TestCheckServiceBackupCoverage(t *testing.T) {
	now := uint64(time.Now().Unix())
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1", Status: "online"}},
		vmRows: map[string][]output.GuestRow{
			"pve1": {
				{Kind: "vm", VMID: 100, Node: "pve1", Tags: "backup"},
				{Kind: "vm", VMID: 101, Node: "pve1", Tags: "backup"},
				{Kind: "vm", VMID: 102, Node: "pve1", Tags: "sandbox"},
			},
		},
		lxcRows: map[string][]output.GuestRow{},
		lxcs:    map[string]map[int]*fakeGuest{},
		storageRows: map[string][]output.StorageRow{
			"pve1": {{Node: "pve1", Storage: "backup", Active: true, Enabled: true, Content: "backup"}},
		},
		backupRows: map[string]map[string][]output.BackupRow{
			"pve1": {
				"backup": {{Node: "pve1", Storage: "backup", Kind: "vm", VMID: 100, CTime: now - 3600}},
			},
		},
	}
	svc := NewCheckService(backend)

	result, err := svc.Run(context.Background(), CheckOptions{
		BackupTag:    "backup",
		BackupMaxAge: 36 * time.Hour,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	byVMID := map[uint64]output.CheckRow{}
	for _, row := range result.Rows {
		if row.Check == "backup-coverage" {
			if vmid, err := parseCheckVMID(row.Resource); err == nil {
				byVMID[vmid] = row
			}
		}
	}
	if row := byVMID[100]; row.Status != output.DoctorStatusOK {
		t.Fatalf("vm 100 row = %#v", row)
	}
	if row := byVMID[101]; row.Status != output.DoctorStatusWarn || row.Message != "no backup found" {
		t.Fatalf("vm 101 row = %#v", row)
	}
	if _, ok := byVMID[102]; ok {
		t.Fatal("untagged guest must not be checked")
	}
}

func TestCheckServiceBackupCoverageDeduplicatesSharedStorage(t *testing.T) {
	now := uint64(time.Now().Unix())
	backend := &fakeBackend{
		nodes: []output.NodeRow{
			{Name: "pve1", Status: "online"},
			{Name: "pve2", Status: "online"},
		},
		vmRows: map[string][]output.GuestRow{
			"pve1": {{Kind: "vm", VMID: 100, Node: "pve1", Tags: "backup"}},
		},
		lxcs:    map[string]map[int]*fakeGuest{},
		lxcRows: map[string][]output.GuestRow{},
		storageRows: map[string][]output.StorageRow{
			"pve1": {{Node: "pve1", Storage: "backup", Type: "nfs", Active: true, Enabled: true, Shared: true, Content: "backup"}},
			"pve2": {{Node: "pve2", Storage: "backup", Type: "nfs", Active: true, Enabled: true, Shared: true, Content: "backup"}},
		},
		backupRows: map[string]map[string][]output.BackupRow{
			"pve1": {
				"backup": {{Node: "pve1", Storage: "backup", Kind: "vm", VMID: 100, CTime: now - 3600}},
			},
		},
	}
	svc := NewCheckService(backend)

	result, err := svc.Run(context.Background(), CheckOptions{
		BackupTag:    "backup",
		BackupMaxAge: 36 * time.Hour,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(backend.backupListCalls) != 1 {
		t.Fatalf("shared storage must be queried once, calls = %#v", backend.backupListCalls)
	}
	if backend.backupListCalls["pve1/backup"] != 1 {
		t.Fatalf("calls = %#v", backend.backupListCalls)
	}
	for _, row := range result.Rows {
		if row.Check == "backup-coverage" && row.Resource == "vm 100" && row.Status != output.DoctorStatusOK {
			t.Fatalf("vm 100 row = %#v", row)
		}
	}
}

func TestCheckServiceBackupCoverageSkippedWithoutOptions(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1", Status: "online"}},
		lxcs:  map[string]map[int]*fakeGuest{},
	}
	svc := NewCheckService(backend)

	result, err := svc.Run(context.Background(), CheckOptions{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, row := range result.Rows {
		if row.Check == "backup-coverage" {
			t.Fatalf("backup coverage must not run without options: %#v", row)
		}
	}
}

func TestCheckServiceNodeFilter(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{
			{Name: "pve1", Status: "online"},
			{Name: "pve2", Status: "offline"},
		},
	}
	svc := NewCheckService(backend)

	result, err := svc.Run(context.Background(), CheckOptions{Node: "pve1"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, row := range result.Rows {
		if row.Resource == "pve2" {
			t.Fatalf("pve2 must be filtered out: %#v", row)
		}
	}
}

func TestCheckServiceUnknownNodeFailsClosed(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1", Status: "online"}},
	}
	svc := NewCheckService(backend)

	result, err := svc.Run(context.Background(), CheckOptions{Node: "pve-does-not-exist"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !result.Failed {
		t.Fatalf("unknown node must fail the check: %#v", result)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("rows = %#v", result.Rows)
	}
	row := result.Rows[0]
	if row.Check != "node-exists" || row.Status != output.DoctorStatusFail || row.Resource != "pve-does-not-exist" {
		t.Fatalf("row = %#v", row)
	}
}

func TestCheckServiceBackupQueryFailureIsNotACoverageGap(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1", Status: "online"}},
		vmRows: map[string][]output.GuestRow{
			"pve1": {{Kind: "vm", VMID: 100, Node: "pve1", Tags: "backup"}},
		},
		lxcs:    map[string]map[int]*fakeGuest{},
		lxcRows: map[string][]output.GuestRow{},
		storageRows: map[string][]output.StorageRow{
			"pve1": {{Node: "pve1", Storage: "backup", Active: true, Enabled: true, Content: "backup"}},
		},
		backupErrs: map[string]error{"pve1/backup": errors.New("timeout")},
	}
	svc := NewCheckService(backend)

	result, err := svc.Run(context.Background(), CheckOptions{
		BackupTag:    "backup",
		BackupMaxAge: 36 * time.Hour,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	var sourceRow, guestRow *output.CheckRow
	for i := range result.Rows {
		row := &result.Rows[i]
		if row.Check != "backup-coverage" {
			continue
		}
		switch {
		case row.Resource == "pve1/backup":
			sourceRow = row
		case parseableGuestResource(row.Resource):
			guestRow = row
		}
	}
	if sourceRow == nil || sourceRow.Status != output.DoctorStatusWarn || sourceRow.Message != "list backups: timeout" {
		t.Fatalf("source row = %#v", sourceRow)
	}
	if guestRow == nil || guestRow.Message != "backup status unavailable" {
		t.Fatalf("guest row = %#v, want backup status unavailable, not no backup found", guestRow)
	}
}

func parseableGuestResource(resource string) bool {
	_, err := parseCheckVMID(resource)
	return err == nil
}

func TestCheckServiceBackupCoveragePartialSourceFailureIsIndeterminate(t *testing.T) {
	now := uint64(time.Now().Unix())
	backend := &fakeBackend{
		nodes: []output.NodeRow{
			{Name: "pve1", Status: "online"},
			{Name: "pve2", Status: "online"},
		},
		vmRows: map[string][]output.GuestRow{
			"pve1": {
				{Kind: "vm", VMID: 100, Node: "pve1", Tags: "backup"},
				{Kind: "vm", VMID: 101, Node: "pve1", Tags: "backup"},
			},
		},
		lxcs:    map[string]map[int]*fakeGuest{},
		lxcRows: map[string][]output.GuestRow{},
		storageRows: map[string][]output.StorageRow{
			"pve1": {{Node: "pve1", Storage: "backup", Active: true, Enabled: true, Content: "backup"}},
			"pve2": {{Node: "pve2", Storage: "vault", Active: true, Enabled: true, Content: "backup"}},
		},
		backupRows: map[string]map[string][]output.BackupRow{
			"pve1": {
				"backup": {{Node: "pve1", Storage: "backup", Kind: "vm", VMID: 100, CTime: now - 3600}},
			},
		},
		backupErrs: map[string]error{"pve2/vault": errors.New("timeout")},
	}
	svc := NewCheckService(backend)

	result, err := svc.Run(context.Background(), CheckOptions{
		BackupTag:    "backup",
		BackupMaxAge: 36 * time.Hour,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	byVMID := map[uint64]output.CheckRow{}
	for _, row := range result.Rows {
		if row.Check == "backup-coverage" {
			if vmid, err := parseCheckVMID(row.Resource); err == nil {
				byVMID[vmid] = row
			}
		}
	}
	// vm 100 has a proven fresh backup, so the failing source cannot
	// overturn its OK verdict.
	if row := byVMID[100]; row.Status != output.DoctorStatusOK {
		t.Fatalf("vm 100 row = %#v", row)
	}
	// vm 101 was not found on the reachable source, but the failing source
	// may hide a newer backup: the verdict must stay indeterminate instead
	// of claiming "no backup found".
	if row := byVMID[101]; row.Status != output.DoctorStatusWarn || row.Message != "backup status unavailable" {
		t.Fatalf("vm 101 row = %#v, want backup status unavailable", row)
	}
}

func TestCheckServiceBackupCoveragePartialFailureBeatsStaleVerdict(t *testing.T) {
	now := uint64(time.Now().Unix())
	backend := &fakeBackend{
		nodes: []output.NodeRow{
			{Name: "pve1", Status: "online"},
			{Name: "pve2", Status: "online"},
		},
		vmRows: map[string][]output.GuestRow{
			"pve1": {{Kind: "vm", VMID: 100, Node: "pve1", Tags: "backup"}},
		},
		lxcs:    map[string]map[int]*fakeGuest{},
		lxcRows: map[string][]output.GuestRow{},
		storageRows: map[string][]output.StorageRow{
			"pve1": {{Node: "pve1", Storage: "backup", Active: true, Enabled: true, Content: "backup"}},
			"pve2": {{Node: "pve2", Storage: "vault", Active: true, Enabled: true, Content: "backup"}},
		},
		backupRows: map[string]map[string][]output.BackupRow{
			"pve1": {
				"backup": {{Node: "pve1", Storage: "backup", Kind: "vm", VMID: 100, CTime: now - 72*3600}},
			},
		},
		backupErrs: map[string]error{"pve2/vault": errors.New("timeout")},
	}
	svc := NewCheckService(backend)

	result, err := svc.Run(context.Background(), CheckOptions{
		BackupTag:    "backup",
		BackupMaxAge: 36 * time.Hour,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, row := range result.Rows {
		if row.Check == "backup-coverage" && row.Resource == "vm 100" {
			// The stale backup on pve1 is not proof that no fresher backup
			// exists on the unqueryable source.
			if row.Status != output.DoctorStatusWarn || row.Message != "backup status unavailable" {
				t.Fatalf("vm 100 row = %#v, want backup status unavailable", row)
			}
			return
		}
	}
	t.Fatalf("no vm 100 coverage row: %#v", result.Rows)
}

func TestCheckServiceBackupCoverageStaleWhenAllSourcesSucceed(t *testing.T) {
	now := uint64(time.Now().Unix())
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1", Status: "online"}},
		vmRows: map[string][]output.GuestRow{
			"pve1": {{Kind: "vm", VMID: 100, Node: "pve1", Tags: "backup"}},
		},
		lxcs:    map[string]map[int]*fakeGuest{},
		lxcRows: map[string][]output.GuestRow{},
		storageRows: map[string][]output.StorageRow{
			"pve1": {{Node: "pve1", Storage: "backup", Active: true, Enabled: true, Content: "backup"}},
		},
		backupRows: map[string]map[string][]output.BackupRow{
			"pve1": {
				"backup": {{Node: "pve1", Storage: "backup", Kind: "vm", VMID: 100, CTime: now - 72*3600}},
			},
		},
	}
	svc := NewCheckService(backend)

	result, err := svc.Run(context.Background(), CheckOptions{
		BackupTag:    "backup",
		BackupMaxAge: 36 * time.Hour,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, row := range result.Rows {
		if row.Check == "backup-coverage" && row.Resource == "vm 100" {
			// With every source queryable, a stale latest backup is a real
			// coverage gap and must keep the stale diagnosis.
			if row.Status != output.DoctorStatusWarn || row.Message != "latest backup is 3d old" {
				t.Fatalf("vm 100 row = %#v, want stale diagnosis", row)
			}
			return
		}
	}
	t.Fatalf("no vm 100 coverage row: %#v", result.Rows)
}
