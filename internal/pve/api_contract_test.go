package pve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	proxmox "github.com/luthermonson/go-proxmox"
	"github.com/lz-wang/pve-cli/v3/internal/config"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestNodePowerHTTPContract(t *testing.T) {
	for _, action := range []string{"reboot", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			b := contractBackend(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/nodes/n/status" {
					t.Errorf("%s %s", r.Method, r.URL.Path)
				}
				var body map[string]any
				if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
					t.Error(e)
				}
				if body["command"] != action || len(body) != 1 {
					t.Errorf("%+v", body)
				}
				if _, e := w.Write([]byte(`{"data":null}`)); e != nil {
					t.Error(e)
				}
			})
			if e := b.PowerNode(context.Background(), "n", action); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestHTTPFailuresTypedAndMutationsNotRetried(t *testing.T) {
	for _, tc := range []struct {
		code int
		kind error
	}{{400, ErrInvalid}, {401, ErrPermissionDenied}, {403, ErrPermissionDenied}, {404, ErrNotFound}, {409, ErrConflict}, {503, ErrUnavailable}} {
		t.Run(fmt.Sprint(tc.code), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.code)
				if _, e := w.Write([]byte(`{"errors":{"password":"echoed-secret"}}`)); e != nil {
					t.Error(e)
				}
			}))
			defer server.Close()
			backend, e := NewProxmoxBackend(config.Profile{Endpoint: server.URL, TokenID: "test@pve!cli"}, ClientOptions{TokenSecret: "private-test-token"})
			if e != nil {
				t.Fatal(e)
			}
			_, e = backend.(GuestCreator).CreateGuest(context.Background(), "vm", "n", 101, map[string]any{})
			if !errors.Is(e, tc.kind) || calls != 1 || strings.Contains(e.Error(), "echoed-secret") {
				t.Fatalf("%v calls=%d", e, calls)
			}
		})
	}
}

// The storage contract reads the cluster storage configuration: a dir
// storage with backup content (string or array form) passes; other types
// or content lists are refused before any host command runs.
func TestVerifyBackupStorageHTTPContract(t *testing.T) {
	configs := map[string]string{
		"/storage/local":    `{"data":{"type":"dir","content":"backup,iso"}}`,
		"/storage/arrayfor": `{"data":{"type":"dir","content":["iso","backup"]}}`,
		"/storage/pbs":      `{"data":{"type":"pbs","content":"backup"}}`,
		"/storage/noarch":   `{"data":{"type":"dir","content":"iso,vztmpl"}}`,
		"/storage/unset":    `{"data":{"type":"dir"}}`,
	}
	b := contractBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/storage/") {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		body, ok := configs[r.URL.Path]
		if !ok {
			t.Errorf("unexpected storage %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	})

	if err := b.VerifyBackupStorage(context.Background(), "local"); err != nil {
		t.Fatalf("dir + backup string content: %v", err)
	}
	if err := b.VerifyBackupStorage(context.Background(), "arrayfor"); err != nil {
		t.Fatalf("dir + backup array content: %v", err)
	}
	if err := b.VerifyBackupStorage(context.Background(), "unset"); err != nil {
		t.Fatalf("unset content list means unrestricted: %v", err)
	}
	if err := b.VerifyBackupStorage(context.Background(), "pbs"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want a non-dir rejection", err)
	}
	if err := b.VerifyBackupStorage(context.Background(), "noarch"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want a missing-backup-content rejection", err)
	}
}
