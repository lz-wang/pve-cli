package pve

import (
	"context"
	"encoding/json"
	proxmox "github.com/luthermonson/go-proxmox"
	"net/http"
	"net/http/httptest"
	"testing"
)

func contractBackend(t *testing.T, handler http.HandlerFunc) *ProxmoxBackend {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &ProxmoxBackend{client: proxmox.NewClient(server.URL)}
}
func TestTemplateDownloadHTTPContract(t *testing.T) {
	calls := 0
	b := contractBackend(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/nodes/n/aplinfo" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		if body["template"] != "debian.tar.zst" || body["storage"] != "local" {
			t.Errorf("%+v", body)
		}
		if _, e := w.Write([]byte(`{"data":"UPID:n:0001:0000:6839F4A1:download:0:root@pam:"}`)); e != nil {
			t.Error(e)
		}
	})
	task, e := b.DownloadTemplate(context.Background(), "n", "local", "debian.tar.zst")
	if e != nil || task == nil || calls != 1 {
		t.Fatalf("%v %v calls=%d", task, e, calls)
	}
}

func TestCreateLXCHTTPContract(t *testing.T) {
	b := contractBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/nodes/n/lxc" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		if body["vmid"] != float64(201) || body["start"] != false || body["unprivileged"] != true {
			t.Errorf("%+v", body)
		}
		if _, e := w.Write([]byte(`{"data":"UPID:n:0001:0000:6839F4A1:vzcreate:201:root@pam:"}`)); e != nil {
			t.Error(e)
		}
	})
	_, e := b.CreateGuest(context.Background(), "lxc", "n", 201, map[string]any{"start": false, "unprivileged": true})
	if e != nil {
		t.Fatal(e)
	}
}

func TestCreateVMHTTPContract(t *testing.T) {
	b := contractBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/nodes/n/qemu" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		if body["vmid"] != float64(101) || body["scsi0"] != "local-lvm:32" || body["start"] != false {
			t.Errorf("%+v", body)
		}
		if _, e := w.Write([]byte(`{"data":"UPID:n:0001:0000:6839F4A1:qmcreate:101:root@pam:"}`)); e != nil {
			t.Error(e)
		}
	})
	_, e := b.CreateGuest(context.Background(), "vm", "n", 101, map[string]any{"start": false, "scsi0": "local-lvm:32"})
	if e != nil {
		t.Fatal(e)
	}
}

func TestDiskUnlinkHTTPNeverForcesAttachedRemoval(t *testing.T) {
	for _, slot := range []string{"scsi1", "unused0"} {
		t.Run(slot, func(t *testing.T) {
			calls := 0
			b := contractBackend(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "PUT" || r.URL.Path != "/nodes/n/qemu/100/unlink" {
					t.Errorf("%s %s", r.Method, r.URL.Path)
				}
				var body map[string]any
				if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
					t.Error(e)
				}
				if body["idlist"] != slot || body["force"] != false || len(body) != 2 {
					t.Errorf("unsafe unlink params: %+v", body)
				}
				if _, e := w.Write([]byte(`{"data":null}`)); e != nil {
					t.Error(e)
				}
			})
			if e := b.UnlinkDisk(context.Background(), "n", 100, slot); e != nil || calls != 1 {
				t.Fatalf("%v %d", e, calls)
			}
		})
	}
}
