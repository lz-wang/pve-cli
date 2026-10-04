package output

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type contractField struct {
	name string
	typ  reflect.Type
}

func TestStructuredOutputContracts(t *testing.T) {
	stringType := reflect.TypeOf("")
	uint64Type := reflect.TypeOf(uint64(0))
	intType := reflect.TypeOf(int(0))
	int64Type := reflect.TypeOf(int64(0))
	float64Type := reflect.TypeOf(float64(0))
	boolType := reflect.TypeOf(false)

	cases := []struct {
		name   string
		value  any
		fields []contractField
	}{
		{
			name: "NodeRow",
			value: NodeRow{
				Name: "pve1", Status: "online", CPU: 0.25, Mem: 1024, MaxMem: 2048,
				Disk: 4096, MaxDisk: 8192, Uptime: 60,
			},
			fields: []contractField{
				{"name", stringType},
				{"status", stringType},
				{"cpu", float64Type},
				{"mem", uint64Type},
				{"max_mem", uint64Type},
				{"disk", uint64Type},
				{"max_disk", uint64Type},
				{"uptime", uint64Type},
			},
		},
		{
			name: "GuestRow",
			value: GuestRow{
				Kind: "vm", VMID: 100, Name: "debian", Node: "pve1", Status: "running",
				CPUs: 2, CPU: 0.5, Mem: 1024, MaxMem: 2048, MaxDisk: 4096, Uptime: 120, Tags: "lab",
			},
			fields: []contractField{
				{"kind", stringType},
				{"vmid", uint64Type},
				{"name", stringType},
				{"node", stringType},
				{"status", stringType},
				{"cpus", intType},
				{"cpu", float64Type},
				{"mem", uint64Type},
				{"max_mem", uint64Type},
				{"max_disk", uint64Type},
				{"uptime", uint64Type},
				{"tags", stringType},
			},
		},
		{
			name: "CloneResult",
			value: CloneResult{
				Kind: "vm", SourceVMID: 9000, NewVMID: 100, SourceNode: "pve1",
				TargetNode: "pve2", Name: "app", Task: "UPID:pve1:clone",
			},
			fields: []contractField{
				{"kind", stringType},
				{"source_vmid", uint64Type},
				{"new_vmid", uint64Type},
				{"source_node", stringType},
				{"target_node", stringType},
				{"name", stringType},
				{"task", stringType},
			},
		},
		{
			name: "SnapshotRow",
			value: SnapshotRow{
				Kind: "vm", VMID: 100, Node: "pve1", Name: "before-upgrade",
				Description: "stable", Parent: "base", Snaptime: 1710000000, VMState: 1, State: "ok",
			},
			fields: []contractField{
				{"kind", stringType},
				{"vmid", uint64Type},
				{"node", stringType},
				{"name", stringType},
				{"description", stringType},
				{"parent", stringType},
				{"snaptime", int64Type},
				{"vmstate", intType},
				{"state", stringType},
			},
		},
		{
			name: "BackupRow",
			value: BackupRow{
				Node: "pve1", Storage: "backup", Kind: "vm", VMID: 100,
				VolID: "backup:backup/vzdump-qemu-100.vma.zst", Format: "vma.zst",
				Size: 1024, Used: 512, CTime: 1710000000, Protected: "1",
				Encrypted: "0", VerifyState: "ok", Notes: "daily",
			},
			fields: []contractField{
				{"node", stringType},
				{"storage", stringType},
				{"kind", stringType},
				{"vmid", uint64Type},
				{"volid", stringType},
				{"format", stringType},
				{"size", uint64Type},
				{"used", uint64Type},
				{"ctime", uint64Type},
				{"protected", stringType},
				{"encrypted", stringType},
				{"verify_state", stringType},
				{"notes", stringType},
			},
		},
		{
			name: "BackupResult",
			value: BackupResult{
				Kind: "vm", VMID: 100, Node: "pve1", Storage: "backup", Mode: "snapshot", Task: "UPID:pve1:backup",
			},
			fields: []contractField{
				{"kind", stringType},
				{"vmid", uint64Type},
				{"node", stringType},
				{"storage", stringType},
				{"mode", stringType},
				{"task", stringType},
			},
		},
		{
			name: "StorageRow",
			value: StorageRow{
				Node: "pve1", Storage: "local", Type: "dir", Active: true, Enabled: true,
				Shared: false, Content: "iso,backup", Used: 1024, Avail: 2048, Total: 3072, UsedFraction: 0.33,
			},
			fields: []contractField{
				{"node", stringType},
				{"storage", stringType},
				{"type", stringType},
				{"active", boolType},
				{"enabled", boolType},
				{"shared", boolType},
				{"content", stringType},
				{"used", uint64Type},
				{"avail", uint64Type},
				{"total", uint64Type},
				{"used_fraction", float64Type},
			},
		},
		{
			name: "StorageContentRow",
			value: StorageContentRow{
				Node: "pve1", Storage: "backup", Content: "backup", VMID: 100,
				VolID: "backup:backup/vzdump-qemu-100.vma.zst", Format: "vma.zst",
				Size: 1024, Used: 512, CTime: 1710000000, Protected: "1",
				Encrypted: "0", VerifyState: "ok", Notes: "daily",
			},
			fields: []contractField{
				{"node", stringType},
				{"storage", stringType},
				{"content", stringType},
				{"vmid", uint64Type},
				{"volid", stringType},
				{"format", stringType},
				{"size", uint64Type},
				{"used", uint64Type},
				{"ctime", uint64Type},
				{"protected", stringType},
				{"encrypted", stringType},
				{"verify_state", stringType},
				{"notes", stringType},
			},
		},
		{
			name: "DoctorRow",
			value: DoctorRow{
				Check: "CONFIG_FILE", Status: DoctorStatusOK, Message: "ok",
			},
			fields: []contractField{
				{"check", stringType},
				{"status", reflect.TypeOf(DoctorStatus(""))},
				{"message", stringType},
			},
		},
		{
			name: "VersionInfo",
			value: VersionInfo{
				Version: "v1.0.0", Commit: "abc1234", Date: "2026-06-06T00:00:00Z",
				GoVersion: "go1.26", OS: "darwin", Arch: "arm64",
			},
			fields: []contractField{
				{"version", stringType},
				{"commit", stringType},
				{"date", stringType},
				{"go_version", stringType},
				{"os", stringType},
				{"arch", stringType},
			},
		},
		{
			name: "TaskRow",
			value: TaskRow{
				UPID: "UPID:pve1:0001:0000:vzdump:100:root@pam:", Node: "pve1", Type: "vzdump",
				ID: "100", User: "root@pam", Status: "ok", ExitStatus: "OK",
				StartTime: 1710000000, EndTime: 1710000600,
			},
			fields: []contractField{
				{"upid", stringType},
				{"node", stringType},
				{"type", stringType},
				{"id", stringType},
				{"user", stringType},
				{"status", stringType},
				{"exit_status", stringType},
				{"start_time", int64Type},
				{"end_time", int64Type},
			},
		},
		{
			name: "TaskLogRow",
			value: TaskLogRow{
				Line: 1, Text: "INFO: starting new backup job",
			},
			fields: []contractField{
				{"line", intType},
				{"text", stringType},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertContractFields(t, tc.value, tc.fields)
			assertSerializedFields(t, tc.value, tc.fields)
		})
	}
}

func assertContractFields(t *testing.T, value any, fields []contractField) {
	t.Helper()
	typ := reflect.TypeOf(value)
	if typ.NumField() != len(fields) {
		t.Fatalf("%s field count = %d, want %d", typ.Name(), typ.NumField(), len(fields))
	}

	for i, want := range fields {
		field := typ.Field(i)
		if got := jsonTagName(field); got != want.name {
			t.Fatalf("%s field %d JSON tag = %q, want %q", typ.Name(), i, got, want.name)
		}
		if got := yamlTagName(field); got != want.name {
			t.Fatalf("%s field %d YAML tag = %q, want %q", typ.Name(), i, got, want.name)
		}
		if field.Type != want.typ {
			t.Fatalf("%s.%s type = %s, want %s", typ.Name(), field.Name, field.Type, want.typ)
		}
	}
}

func assertSerializedFields(t *testing.T, value any, fields []contractField) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %T: %v", value, err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal %T: %v", value, err)
	}
	for _, field := range fields {
		if _, ok := got[field.name]; !ok {
			t.Fatalf("%T JSON missing field %q in %s", value, field.name, string(data))
		}
	}

	data, err = yaml.Marshal(value)
	if err != nil {
		t.Fatalf("marshal yaml %T: %v", value, err)
	}
	got = map[string]any{}
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal yaml %T: %v", value, err)
	}
	for _, field := range fields {
		if _, ok := got[field.name]; !ok {
			t.Fatalf("%T YAML missing field %q in %s", value, field.name, string(data))
		}
	}
}

func jsonTagName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "" || tag == "-" {
		return ""
	}
	return strings.Split(tag, ",")[0]
}

func yamlTagName(field reflect.StructField) string {
	tag := field.Tag.Get("yaml")
	if tag == "" || tag == "-" {
		return ""
	}
	return strings.Split(tag, ",")[0]
}
