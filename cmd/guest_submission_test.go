package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
)

func TestGuestSubmissionErrorContract(t *testing.T) {
	for _, kind := range []string{"vm", "lxc"} {
		for _, action := range []string{"backup", "restore"} {
			for _, tc := range []struct {
				name             string
				httpStatus, exit int
				status           string
			}{
				{"rejected", 400, 2, "failed"},
				{"server_error", 500, 5, "unknown"},
				{"request_timeout", 408, 5, "unknown"},
				{"client_timeout", 0, 5, "unknown"},
			} {
				t.Run(kind+"/"+action+"/"+tc.name, func(t *testing.T) {
					var submissions atomic.Int32
					release := make(chan struct{})
					guestPath := "qemu"
					if kind == "lxc" {
						guestPath = "lxc"
					}
					mutationPath := "/nodes/pve1/vzdump"
					if action == "restore" {
						mutationPath = "/nodes/pve1/" + guestPath
					}
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						path := strings.TrimPrefix(r.URL.Path, "/api2/json")
						switch {
						case r.Method == http.MethodPost && path == mutationPath:
							submissions.Add(1)
							if tc.httpStatus == 0 {
								<-release
								return
							}
							w.WriteHeader(tc.httpStatus)
							_, _ = w.Write([]byte(`{"data":null,"message":"submission rejected"}`))
						case r.Method == http.MethodGet && path == "/nodes":
							_, _ = w.Write([]byte(`{"data":[{"node":"pve1","status":"online"}]}`))
						case r.Method == http.MethodGet && path == "/nodes/pve1/status":
							_, _ = w.Write([]byte(`{"data":{"status":"online"}}`))
						case r.Method == http.MethodGet && strings.HasSuffix(path, "/100/config"):
							_, _ = w.Write([]byte(`{"data":{}}`))
						case r.Method == http.MethodGet && strings.HasSuffix(path, "/100/status/current"):
							_, _ = w.Write([]byte(`{"data":{"vmid":100,"name":"test","status":"stopped"}}`))
						case r.Method == http.MethodGet && (path == "/nodes/pve1/qemu" || path == "/nodes/pve1/lxc" || path == "/cluster/resources"):
							if action == "restore" {
								_, _ = w.Write([]byte(`{"data":[]}`))
							} else {
								_, _ = fmt.Fprintf(w, `{"data":[{"vmid":100,"node":"pve1","type":%q,"name":"test","status":"stopped"}]}`, guestPath)
							}
						default:
							t.Errorf("unexpected request: %s %s", r.Method, path)
							w.WriteHeader(http.StatusNotFound)
						}
					}))
					defer server.Close()
					defer close(release)
					backend, err := pve.NewProxmoxBackend(config.Profile{Endpoint: server.URL, TokenID: "test@pve!cli"}, pve.ClientOptions{TokenSecret: "test-secret", Timeout: time.Second})
					if err != nil {
						t.Fatal(err)
					}
					var stdout, stderr bytes.Buffer
					deps := Dependencies{Stdout: &stdout, Stderr: &stderr, BackendFactory: func(config.Profile, pve.ClientOptions) (pve.Backend, error) { return backend, nil }}
					args := []string{"pve", "--config", writeTwoProfileConfig(t), "--profile", "n150", kind, action}
					if action == "backup" {
						args = append(args, "100", "--node", "pve1", "--storage", "backup")
					} else {
						args = append(args, "/tmp/example.tar.zst", "--node", "pve1", "--vmid", "201")
					}
					args = append(args, "-o", "json")
					err = RunWithDependencies(args, "test", deps)
					if err == nil || ExitCode(err) != tc.exit {
						t.Fatalf("exit=%d want=%d err=%v stdout=%s", ExitCode(err), tc.exit, err, stdout.String())
					}
					var result map[string]any
					if e := json.Unmarshal(stdout.Bytes(), &result); e != nil {
						t.Fatalf("invalid JSON: %v: %s", e, stdout.String())
					}
					if result["status"] != tc.status || result["resource"] != kind || result["kind"] != kind {
						t.Fatalf("result=%v", result)
					}
					if message, ok := result["error"].(string); !ok || message == "" {
						t.Fatalf("missing error: %v", result)
					}
					if task, ok := result["task"]; ok && task != "" {
						t.Fatalf("unexpected task for failed HTTP response: %v", task)
					}
					if action == "backup" {
						if result["effective_mode"] != "unknown" || result["requested_mode"] != "snapshot" {
							t.Fatalf("backup result=%v", result)
						}
					} else if result["restore_status"] != tc.status || result["post_config_status"] != "skipped" {
						t.Fatalf("restore result=%v", result)
					}
					if submissions.Load() != 1 {
						t.Fatalf("submission count=%d, want exactly one", submissions.Load())
					}
				})
			}
		}
	}
}
