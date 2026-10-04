package pve

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lz-wang/pvectl/internal/output"
)

func TestCloudInitValuesRequireAtLeastOneOption(t *testing.T) {
	if _, err := cloudInitValues(CloudInitSetOptions{}); err == nil {
		t.Fatal("expected empty options error")
	}
}

func TestCloudInitValuesReadsPasswordFromEnv(t *testing.T) {
	t.Setenv("VM_PASSWORD", "s3cret")

	values, err := cloudInitValues(CloudInitSetOptions{PasswordEnv: "VM_PASSWORD"})
	if err != nil {
		t.Fatalf("values: %v", err)
	}
	if values["cipassword"] != "s3cret" {
		t.Fatalf("cipassword = %q", values["cipassword"])
	}

	if _, err := cloudInitValues(CloudInitSetOptions{PasswordEnv: "VM_MISSING_PASSWORD"}); err == nil {
		t.Fatal("expected missing env error")
	}
}

func TestCloudInitValuesEncodesSSHKeysFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "id.pub")
	content := "# lab keys\nssh-ed25519 AAAAtest key@host\n\nssh-rsa AAAArsa other@host\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	values, err := cloudInitValues(CloudInitSetOptions{SSHKeyFile: path})
	if err != nil {
		t.Fatalf("values: %v", err)
	}
	if values["sshkeys"] == "" || strings.Contains(values["sshkeys"], "\n") {
		t.Fatalf("sshkeys = %q", values["sshkeys"])
	}
	// Decoding must reproduce every full key line; splitting on whitespace
	// would shred each "type base64 comment" key into fragments.
	decoded, err := url.QueryUnescape(values["sshkeys"])
	if err != nil {
		t.Fatalf("decode sshkeys %q: %v", values["sshkeys"], err)
	}
	want := "ssh-ed25519 AAAAtest key@host\nssh-rsa AAAArsa other@host"
	if decoded != want {
		t.Fatalf("decoded sshkeys = %q, want %q", decoded, want)
	}

	empty := filepath.Join(t.TempDir(), "empty.pub")
	if err := os.WriteFile(empty, []byte(""), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	if _, err := cloudInitValues(CloudInitSetOptions{SSHKeyFile: empty}); err == nil {
		t.Fatal("expected empty key file error")
	}
}

func TestCloudInitValuesValidatesIPConfigs(t *testing.T) {
	values, err := cloudInitValues(CloudInitSetOptions{IPConfigs: map[string]string{"ipconfig0": "ip=dhcp"}})
	if err != nil {
		t.Fatalf("values: %v", err)
	}
	if values["ipconfig0"] != "ip=dhcp" {
		t.Fatalf("values = %#v", values)
	}

	if _, err := cloudInitValues(CloudInitSetOptions{IPConfigs: map[string]string{"net0": "ip=dhcp"}}); err == nil {
		t.Fatal("expected invalid device error")
	}
}

func TestCloudInitServiceSetDelegatesToVMConfig(t *testing.T) {
	task := &fakeTask{upid: "UPID:pve1:cloudinit"}
	guest := &fakeGuest{
		row:  output.GuestRow{Kind: "vm", VMID: 100, Node: "pve1"},
		task: task,
	}
	backend := &fakeBackend{
		nodes: []output.NodeRow{{Name: "pve1"}},
		vms:   map[string]map[int]*fakeGuest{"pve1": {100: guest}},
	}
	svc := NewCloudInitService(backend, TaskRunner{Wait: true}, nil, false)

	err := svc.Set(context.Background(), 100, "", CloudInitSetOptions{User: "debian"})
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if guest.configValues["ciuser"] != "debian" {
		t.Fatalf("config values = %#v", guest.configValues)
	}
	if !task.waited {
		t.Fatal("expected cloud-init set to be waited")
	}
}
