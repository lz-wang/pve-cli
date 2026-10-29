package pve

import (
	"context"
	"testing"
)

func TestDiskRolesAndWrongTargets(t *testing.T) {
	rows := ParseDisks(map[string]any{"boot": "order=scsi0;net0", "scsi0": "pool:vm-100-disk-0,size=32G", "scsi1": "pool:vm-100-disk-1,size=20G", "unused0": "pool:vm-100-disk-2", "ide2": "local:iso/debian.iso,media=cdrom"}, 100, "n")
	roles := map[string]string{}
	for _, r := range rows {
		roles[r.Slot] = r.Role
	}
	if roles["scsi0"] != "system" || roles["scsi1"] != "attached" || roles["unused0"] != "unused" || roles["ide2"] != "installer" {
		t.Fatal(roles)
	}
	s := NewDiskService(nil, TaskRunner{})
	if _, e := s.Unlink(context.Background(), 100, "n", "unused0", false); e == nil {
		t.Fatal("remove accepted unused disk")
	}
	if _, e := s.Unlink(context.Background(), 100, "n", "scsi0", true); e == nil {
		t.Fatal("delete accepted attached disk")
	}
}
