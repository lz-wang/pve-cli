package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestValidateFormat(t *testing.T) {
	for _, format := range []string{"table", "json", "yaml"} {
		if err := ValidateFormat(format); err != nil {
			t.Fatalf("format %q: %v", format, err)
		}
	}
	if err := ValidateFormat("xml"); err == nil {
		t.Fatal("expected invalid format")
	}
}

func TestWriteGuestRowsJSON(t *testing.T) {
	var buf bytes.Buffer
	rows := []GuestRow{{Kind: "vm", VMID: 100, Name: "debian", Node: "pve1", Status: "running"}}

	if err := WriteGuestRows(&buf, "json", rows); err != nil {
		t.Fatalf("write json: %v", err)
	}
	if !strings.Contains(buf.String(), `"vmid": 100`) {
		t.Fatalf("json output = %s", buf.String())
	}
}

func TestWriteGuestRowsTable(t *testing.T) {
	var buf bytes.Buffer
	rows := []GuestRow{{Kind: "vm", VMID: 100, Name: "debian", Node: "pve1", Status: "running", MaxMem: 1024 * 1024 * 1024}}

	if err := WriteGuestRows(&buf, "table", rows); err != nil {
		t.Fatalf("write table: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "VMID") || !strings.Contains(got, "1.0GiB") {
		t.Fatalf("table output = %s", got)
	}
	if strings.Contains(got, "KIND") {
		t.Fatalf("regular guest table should not include KIND: %s", got)
	}
}

func TestWriteDoctorRowsTableJSONAndYAML(t *testing.T) {
	rows := []DoctorRow{
		{Check: "CONFIG_PATH", Status: DoctorStatusOK, Message: "/tmp/config.yaml"},
		{Check: "TOKEN_SECRET_ENV", Status: DoctorStatusFail, Message: "environment variable PVE_TOKEN is empty"},
	}

	var table bytes.Buffer
	if err := WriteDoctorRows(&table, "table", rows); err != nil {
		t.Fatalf("write doctor table: %v", err)
	}
	if got := table.String(); !strings.Contains(got, "CHECK") || !strings.Contains(got, "TOKEN_SECRET_ENV") || !strings.Contains(got, "fail") {
		t.Fatalf("table output = %s", got)
	}

	var jsonBuf bytes.Buffer
	if err := WriteDoctorRows(&jsonBuf, "json", rows); err != nil {
		t.Fatalf("write doctor json: %v", err)
	}
	if got := jsonBuf.String(); !strings.Contains(got, `"check": "CONFIG_PATH"`) || !strings.Contains(got, `"status": "fail"`) {
		t.Fatalf("json output = %s", got)
	}

	var yamlBuf bytes.Buffer
	if err := WriteDoctorRows(&yamlBuf, "yaml", rows); err != nil {
		t.Fatalf("write doctor yaml: %v", err)
	}
	if got := yamlBuf.String(); !strings.Contains(got, "check: CONFIG_PATH") || !strings.Contains(got, "status: fail") {
		t.Fatalf("yaml output = %s", got)
	}
}

func TestWriteGuestRowsWithKindTable(t *testing.T) {
	var buf bytes.Buffer
	rows := []GuestRow{{Kind: "lxc", VMID: 200, Name: "app", Node: "pve1", Status: "running", MaxMem: 2 * 1024 * 1024 * 1024}}

	if err := WriteGuestRowsWithKind(&buf, "table", rows); err != nil {
		t.Fatalf("write table: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "KIND") || !strings.Contains(got, "lxc") || !strings.Contains(got, "2.0GiB") {
		t.Fatalf("table output = %s", got)
	}
}

func TestWriteCloneResultJSON(t *testing.T) {
	var buf bytes.Buffer
	result := CloneResult{Kind: "vm", SourceVMID: 9000, NewVMID: 101, SourceNode: "pve1", TargetNode: "pve2", Name: "app-vm"}

	if err := WriteCloneResult(&buf, "json", result); err != nil {
		t.Fatalf("write clone json: %v", err)
	}
	if !strings.Contains(buf.String(), `"new_vmid": 101`) {
		t.Fatalf("json output = %s", buf.String())
	}
}

func TestWriteCloneResultTable(t *testing.T) {
	var buf bytes.Buffer
	result := CloneResult{Kind: "lxc", SourceVMID: 900, NewVMID: 201, SourceNode: "pve1", TargetNode: "pve1", Name: "app-lxc"}

	if err := WriteCloneResult(&buf, "table", result); err != nil {
		t.Fatalf("write clone table: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "NEWID") || !strings.Contains(got, "201") {
		t.Fatalf("table output = %s", got)
	}
}

func TestWriteSnapshotRowsJSON(t *testing.T) {
	var buf bytes.Buffer
	rows := []SnapshotRow{{Kind: "vm", VMID: 101, Node: "pve1", Name: "before-upgrade", Snaptime: 1710000000}}

	if err := WriteSnapshotRows(&buf, "json", rows); err != nil {
		t.Fatalf("write snapshot json: %v", err)
	}
	if !strings.Contains(buf.String(), `"name": "before-upgrade"`) {
		t.Fatalf("json output = %s", buf.String())
	}
}

func TestWriteSnapshotRowsTable(t *testing.T) {
	var buf bytes.Buffer
	rows := []SnapshotRow{{Kind: "lxc", VMID: 201, Node: "pve1", Name: "before-upgrade", Parent: "base", Snaptime: 1710000000}}

	if err := WriteSnapshotRows(&buf, "table", rows); err != nil {
		t.Fatalf("write snapshot table: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "SNAPTIME") || !strings.Contains(got, "before-upgrade") {
		t.Fatalf("table output = %s", got)
	}
}

func TestWriteBackupRowsJSONOmitsEmptyOptionalFields(t *testing.T) {
	var buf bytes.Buffer
	rows := []BackupRow{{
		Node:    "pve1",
		Storage: "backup",
		Kind:    "vm",
		VMID:    100,
		VolID:   "backup:backup/vzdump-qemu-100.vma.zst",
		Format:  "vma.zst",
		Size:    1024,
		CTime:   1710000000,
	}}

	if err := WriteBackupRows(&buf, "json", rows); err != nil {
		t.Fatalf("write backup json: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, `"volid": "backup:backup/vzdump-qemu-100.vma.zst"`) {
		t.Fatalf("json output = %s", got)
	}
	if strings.Contains(got, "protected") || strings.Contains(got, "verify_state") {
		t.Fatalf("expected optional fields to be omitted: %s", got)
	}
}

func TestWriteBackupRowsTable(t *testing.T) {
	var buf bytes.Buffer
	rows := []BackupRow{{
		Node:        "pve1",
		Storage:     "backup",
		Kind:        "lxc",
		VMID:        200,
		VolID:       "backup:backup/vzdump-lxc-200.tar.zst",
		Format:      "tar.zst",
		Size:        2 * 1024 * 1024 * 1024,
		CTime:       1710000000,
		Protected:   "1",
		VerifyState: "ok",
	}}

	if err := WriteBackupRows(&buf, "table", rows); err != nil {
		t.Fatalf("write backup table: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "NODE") || !strings.Contains(got, "2.0GiB") || !strings.Contains(got, "VERIFY") {
		t.Fatalf("table output = %s", got)
	}
}

func TestWriteBackupResultTableAndJSON(t *testing.T) {
	var table bytes.Buffer
	result := BackupResult{Kind: "vm", VMID: 100, Node: "pve1", Storage: "backup", Mode: "snapshot", Task: "UPID:pve1:backup"}

	if err := WriteBackupResult(&table, "table", result); err != nil {
		t.Fatalf("write backup result table: %v", err)
	}
	if got := table.String(); !strings.Contains(got, "TASK") || !strings.Contains(got, "UPID:pve1:backup") {
		t.Fatalf("table output = %s", got)
	}

	var jsonBuf bytes.Buffer
	if err := WriteBackupResult(&jsonBuf, "json", result); err != nil {
		t.Fatalf("write backup result json: %v", err)
	}
	if !strings.Contains(jsonBuf.String(), `"task": "UPID:pve1:backup"`) {
		t.Fatalf("json output = %s", jsonBuf.String())
	}
}

func TestWriteStorageRowsTable(t *testing.T) {
	var buf bytes.Buffer
	rows := []StorageRow{{
		Node:         "pve1",
		Storage:      "local",
		Type:         "dir",
		Active:       true,
		Enabled:      true,
		Content:      "iso,backup",
		Used:         1024 * 1024 * 1024,
		Avail:        3 * 1024 * 1024 * 1024,
		Total:        4 * 1024 * 1024 * 1024,
		UsedFraction: 0.25,
	}}

	if err := WriteStorageRows(&buf, "table", rows); err != nil {
		t.Fatalf("write storage table: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "USED%") || !strings.Contains(got, "yes") || !strings.Contains(got, "1.0GiB") || !strings.Contains(got, "25.0") {
		t.Fatalf("table output = %s", got)
	}
}

func TestWriteStorageDetailTableAndJSON(t *testing.T) {
	row := StorageRow{Node: "pve1", Storage: "local-lvm", Type: "lvmthin", Active: true, Enabled: true, Shared: false, UsedFraction: 0.5}

	var table bytes.Buffer
	if err := WriteStorageDetail(&table, "table", row); err != nil {
		t.Fatalf("write storage detail table: %v", err)
	}
	if got := table.String(); !strings.Contains(got, "Storage:") || !strings.Contains(got, "local-lvm") || !strings.Contains(got, "Used%") {
		t.Fatalf("table output = %s", got)
	}

	var jsonBuf bytes.Buffer
	if err := WriteStorageDetail(&jsonBuf, "json", row); err != nil {
		t.Fatalf("write storage detail json: %v", err)
	}
	if !strings.Contains(jsonBuf.String(), `"used_fraction": 0.5`) {
		t.Fatalf("json output = %s", jsonBuf.String())
	}
}

func TestWriteStorageContentRowsTableAndJSON(t *testing.T) {
	rows := []StorageContentRow{{
		Node:        "pve1",
		Storage:     "backup",
		Content:     "backup",
		VMID:        100,
		VolID:       "backup:backup/vzdump-qemu-100.vma.zst",
		Format:      "vma.zst",
		Size:        2 * 1024 * 1024 * 1024,
		CTime:       1710000000,
		Protected:   "1",
		VerifyState: "ok",
	}}

	var table bytes.Buffer
	if err := WriteStorageContentRows(&table, "table", rows); err != nil {
		t.Fatalf("write storage content table: %v", err)
	}
	got := table.String()
	if !strings.Contains(got, "CONTENT") || !strings.Contains(got, "2.0GiB") || !strings.Contains(got, "VERIFY") {
		t.Fatalf("table output = %s", got)
	}

	var jsonBuf bytes.Buffer
	if err := WriteStorageContentRows(&jsonBuf, "json", rows); err != nil {
		t.Fatalf("write storage content json: %v", err)
	}
	if !strings.Contains(jsonBuf.String(), `"volid": "backup:backup/vzdump-qemu-100.vma.zst"`) {
		t.Fatalf("json output = %s", jsonBuf.String())
	}
}

func TestFormatUptime(t *testing.T) {
	if got := FormatUptime(0); got != "-" {
		t.Fatalf("zero uptime = %q", got)
	}
	if got := FormatUptime(3*24*60*60 + 4*60*60 + 5*60); got != "3d4h5m" {
		t.Fatalf("uptime = %q", got)
	}
}
