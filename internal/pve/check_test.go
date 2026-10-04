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
