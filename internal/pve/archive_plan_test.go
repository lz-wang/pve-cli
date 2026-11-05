package pve

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

type archivePlanFake struct {
	*restoreTestBackend
	archiveConfig map[string]any
}

func (b archivePlanFake) ArchiveConfig(context.Context, string, string) (map[string]any, error) {
	return b.archiveConfig, nil
}
func TestArchivePlanDiskCapacityAndDependencies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		disk   string
		bridge string
		want   string
	}{
		{"sufficient", "size=8G", "vmbr0", "warning"}, {"virtual size exceeds compressed archive", "size=101G", "vmbr0", "failed"}, {"bridge missing", "size=8G", "missing", "failed"}, {"size unknown", "size=NaN", "vmbr0", "warning"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := archivePlanFake{newRestoreTestBackend(), map[string]any{"rootfs": "local-lvm:vm-101-disk-0," + tc.disk, "net0": "name=eth0,bridge=" + tc.bridge, "mp0": "/data,mp=/data,backup=0"}}
			plan, err := PlanArchiveRestore(context.Background(), b, b, ArchivePlanOptions{SourceNode: "pve1", RestoreOptions: restorePlanOptions()})
			if err != nil || plan.Status != tc.want {
				t.Fatalf("%v %+v", err, plan)
			}
			var external bool
			for _, c := range plan.Checks {
				if c.Check == "external_data" {
					external = true
				}
			}
			if !external {
				t.Fatal("missing excluded mount finding")
			}
			if b.restoreOptions.Archive != "" {
				t.Fatal("plan submitted restore")
			}
		})
	}
}
func TestArchivePlanMapsAndCombinedCapacity(t *testing.T) {
	b := archivePlanFake{newRestoreTestBackend(), map[string]any{"rootfs": "old:x,size=60G", "mp1": "old:y,size=60G,backup=1", "net0": "name=eth0,bridge=old"}}
	o := ArchivePlanOptions{SourceNode: "pve1", RestoreOptions: restorePlanOptions()}
	o.BridgeMap = map[string]string{"old": "vmbr0"}
	o.DiskStorage = map[string]string{"rootfs": "local-lvm", "mp1": "local-lvm", "mp9": "local-lvm"}
	plan, err := PlanArchiveRestore(context.Background(), b, b, o)
	if err != nil || plan.Status != "failed" {
		t.Fatalf("%+v %v", plan, err)
	}
	checks := map[string]string{}
	for _, c := range plan.Checks {
		checks[c.Check] = c.Status
	}
	if checks["space.local-lvm"] != "fail" || checks["network.net0"] != "ok" || checks["disk_map.mp9"] != "fail" {
		t.Fatal(checks)
	}
	b.vmRows = map[string][]output.GuestRow{"pve1": {{VMID: 201}}}
	plan, _ = PlanArchiveRestore(context.Background(), b, b, o)
	if plan.Checks[0].Status != "fail" {
		t.Fatal("occupied VMID accepted")
	}
}
func TestArchiveConfigHTTPAndParser(t *testing.T) {
	archive := "backup:backup/vzdump-lxc-101-test.tar.zst"
	b := contractBackend(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/nodes/pve1/vzdump/extractconfig" || r.URL.Query().Get("volume") != archive {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "# comment\nrootfs: local:x,size=8G\npassword: PRIVATE\n[snapshot]\nrootfs: old:size=2G\n"})
	}))
	cfg, err := b.ArchiveConfig(context.Background(), "pve1", archive)
	if err != nil || cfg["rootfs"] != "local:x,size=8G" {
		t.Fatalf("%v %v", cfg, err)
	}
	for _, raw := range []string{"", "rootfs: x\nrootfs: y", "malformed PRIVATE", strings.Repeat("x", (1<<20)+1)} {
		_, err := parseArchiveConfig(raw)
		if err == nil || strings.Contains(err.Error(), "PRIVATE") {
			t.Fatalf("unsafe parser error %v", err)
		}
	}
	for _, size := range []string{"NaN", "Inf", "-1G", "999999999999999999999999999999999999999999999999999999T", "0G"} {
		if _, ok := safeArchiveDiskSize("size=" + size); ok {
			t.Fatalf("invalid size %s", size)
		}
	}
}
