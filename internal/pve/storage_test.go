package pve

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

func TestStorageServiceListAllNodesFiltersSortsAndSkipsFailures(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve3"}, {Name: "pve2"}, {Name: "pve1"}},
		storageRows: map[string][]output.StorageRow{
			"pve1": {
				{Node: "pve1", Storage: "zfs", Type: "zfspool", Active: true, Enabled: true, Content: "images"},
				{Node: "pve1", Storage: "backup", Type: "dir", Active: true, Enabled: true, Content: "backup,iso"},
			},
			"pve3": {
				{Node: "pve3", Storage: "offline", Type: "dir", Active: false, Enabled: true, Content: "backup"},
				{Node: "pve3", Storage: "local", Type: "dir", Active: true, Enabled: true, Content: "iso, backup"},
			},
		},
		storageErrs: map[string]error{"pve2": errors.New("forbidden")},
	}
	svc := NewStorageService(backend)

	rows, err := svc.List(context.Background(), StorageListOptions{
		Content: "backup",
		Type:    "dir",
		Active:  true,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertStorageOrder(t, rows, []string{"pve1/backup", "pve3/local"})
	if backend.nodeCalls != 1 || backend.storageCalls["pve1"] != 1 || backend.storageCalls["pve2"] != 1 || backend.storageCalls["pve3"] != 1 {
		t.Fatalf("calls nodes=%d storage=%#v", backend.nodeCalls, backend.storageCalls)
	}
}

func TestStorageServiceListExplicitNodeOnlyQueriesThatNode(t *testing.T) {
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		storageRows: map[string][]output.StorageRow{
			"pve2": {{Node: "pve2", Storage: "backup", Type: "nfs"}},
		},
	}
	svc := NewStorageService(backend)

	rows, err := svc.List(context.Background(), StorageListOptions{Node: "pve2"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertStorageOrder(t, rows, []string{"pve2/backup"})
	if backend.nodeCalls != 0 || backend.storageCalls["pve2"] != 1 {
		t.Fatalf("calls nodes=%d storage=%#v", backend.nodeCalls, backend.storageCalls)
	}
}

func TestStorageServiceListAllNodesFailsWhenEveryNodeFails(t *testing.T) {
	backend := &fakeBackend{
		nodes:       []output.NodeRow{{Name: "pve1"}, {Name: "pve2"}},
		storageErrs: map[string]error{"pve1": errors.New("down"), "pve2": errors.New("forbidden")},
	}
	svc := NewStorageService(backend)

	_, err := svc.List(context.Background(), StorageListOptions{})
	if err == nil || !strings.Contains(err.Error(), "list storage: no nodes could be queried: down") {
		t.Fatalf("error = %v", err)
	}
}

func TestStorageServiceGetRequiresNodeAndStorage(t *testing.T) {
	svc := NewStorageService(&fakeBackend{})

	if _, err := svc.Get(context.Background(), "", "local"); err == nil || err.Error() != "node is required" {
		t.Fatalf("node error = %v", err)
	}
	if _, err := svc.Get(context.Background(), "pve1", " "); err == nil || err.Error() != "storage is required" {
		t.Fatalf("storage error = %v", err)
	}
}

func TestStorageServiceGetReturnsBackendRow(t *testing.T) {
	backend := &fakeBackend{
		storageByName: map[string]map[string]output.StorageRow{
			"pve1": {"local": {Node: "pve1", Storage: "local", Type: "dir", Active: true}},
		},
	}
	svc := NewStorageService(backend)

	row, err := svc.Get(context.Background(), "pve1", "local")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.Node != "pve1" || row.Storage != "local" || !row.Active {
		t.Fatalf("row = %#v", row)
	}
}

func TestStorageServiceListContentRequiresNodeStorageAndValidVMID(t *testing.T) {
	svc := NewStorageService(&fakeBackend{})

	if _, err := svc.ListContent(context.Background(), StorageContentListOptions{Storage: "local"}); err == nil || err.Error() != "node is required" {
		t.Fatalf("node error = %v", err)
	}
	if _, err := svc.ListContent(context.Background(), StorageContentListOptions{Node: "pve1"}); err == nil || err.Error() != "storage is required" {
		t.Fatalf("storage error = %v", err)
	}
	if _, err := svc.ListContent(context.Background(), StorageContentListOptions{Node: "pve1", Storage: "local", VMID: -1}); err == nil || err.Error() != "invalid vmid -1" {
		t.Fatalf("vmid error = %v", err)
	}
}

func TestStorageServiceListContentFiltersAndSorts(t *testing.T) {
	backend := &fakeBackend{
		storageContent: map[string]map[string][]output.StorageContentRow{
			"pve1": {
				"local": {
					{Node: "pve1", Storage: "local", Content: "iso", VolID: "local:iso/debian.iso"},
					{Node: "pve1", Storage: "local", Content: "backup", VMID: 100, CTime: 10, VolID: "local:backup/vzdump-qemu-100-old.vma.zst"},
					{Node: "pve1", Storage: "local", Content: "images", VMID: 100, VolID: "local:vm-100-disk-0"},
					{Node: "pve1", Storage: "local", Content: "backup", VMID: 100, CTime: 20, VolID: "local:backup/vzdump-qemu-100-new.vma.zst"},
					{Node: "pve1", Storage: "local", Content: "backup", VMID: 200, CTime: 30, VolID: "local:backup/vzdump-lxc-200.tar.zst"},
				},
			},
		},
	}
	svc := NewStorageService(backend)

	rows, err := svc.ListContent(context.Background(), StorageContentListOptions{
		Node:    "pve1",
		Storage: "local",
		Content: "backup",
		VMID:    100,
	})
	if err != nil {
		t.Fatalf("list content: %v", err)
	}
	assertStorageContentOrder(t, rows, []string{
		"backup/100/20/local:backup/vzdump-qemu-100-new.vma.zst",
		"backup/100/10/local:backup/vzdump-qemu-100-old.vma.zst",
	})
}

func TestStorageServiceListContentSortsByContentVMIDTimeAndVolID(t *testing.T) {
	backend := &fakeBackend{
		storageContent: map[string]map[string][]output.StorageContentRow{
			"pve1": {
				"local": {
					{Content: "iso", VolID: "local:iso/debian.iso"},
					{Content: "backup", VMID: 200, CTime: 10, VolID: "b"},
					{Content: "backup", VMID: 100, CTime: 10, VolID: "c"},
					{Content: "backup", VMID: 100, CTime: 20, VolID: "a"},
					{Content: "backup", VMID: 100, CTime: 20, VolID: "0"},
				},
			},
		},
	}
	svc := NewStorageService(backend)

	rows, err := svc.ListContent(context.Background(), StorageContentListOptions{Node: "pve1", Storage: "local"})
	if err != nil {
		t.Fatalf("list content: %v", err)
	}
	assertStorageContentOrder(t, rows, []string{
		"backup/100/20/0",
		"backup/100/20/a",
		"backup/100/10/c",
		"backup/200/10/b",
		"iso/0/0/local:iso/debian.iso",
	})
}

func TestInferStorageContentTypeAndVMID(t *testing.T) {
	cases := map[string]string{
		"local:iso/debian.iso":                  "iso",
		"local:vztmpl/debian-12.tar.zst":        "vztmpl",
		"backup:backup/vzdump-qemu-100.vma.zst": "backup",
		"local:snippets/user-data.yaml":         "snippets",
		"local:import/disk.qcow2":               "import",
		"local-lvm:vm-101-disk-0":               "images",
		"local-lvm:base-9000-disk-0":            "images",
		"local:subvol-200-disk-0":               "rootdir",
		"local:unrecognized":                    "unknown",
	}
	for volid, want := range cases {
		if got := inferStorageContentType(volid); got != want {
			t.Fatalf("type %q = %q, want %q", volid, got, want)
		}
	}

	vmidCases := map[string]uint64{
		"local-lvm:vm-101-disk-0":               101,
		"local-lvm:base-9000-disk-0":            9000,
		"local:subvol-200-disk-0":               200,
		"backup:backup/vzdump-qemu-300.vma.zst": 300,
		"backup:backup/vzdump-lxc-400.tar.zst":  400,
		"local:iso/debian.iso":                  0,
	}
	for volid, want := range vmidCases {
		if got := inferStorageContentVMID(0, volid); got != want {
			t.Fatalf("vmid %q = %d, want %d", volid, got, want)
		}
	}
	if got := inferStorageContentVMID(123, "local:iso/debian.iso"); got != 123 {
		t.Fatalf("explicit vmid = %d", got)
	}
}

func assertStorageOrder(t *testing.T, rows []output.StorageRow, want []string) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("rows len = %d, want %d: %#v", len(rows), len(want), rows)
	}
	for i, row := range rows {
		got := row.Node + "/" + row.Storage
		if got != want[i] {
			t.Fatalf("row %d = %s, want %s; rows = %#v", i, got, want[i], rows)
		}
	}
}

func assertStorageContentOrder(t *testing.T, rows []output.StorageContentRow, want []string) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("rows len = %d, want %d: %#v", len(rows), len(want), rows)
	}
	for i, row := range rows {
		got := strings.Join([]string{
			row.Content,
			strconvFormatUint(row.VMID),
			strconvFormatUint(row.CTime),
			row.VolID,
		}, "/")
		if got != want[i] {
			t.Fatalf("row %d = %s, want %s; rows = %#v", i, got, want[i], rows)
		}
	}
}
