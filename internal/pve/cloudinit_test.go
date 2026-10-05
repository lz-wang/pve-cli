package pve

import (
	"context"
	"testing"

	"github.com/lz-wang/pve-cli/v2/internal/output"
)

func TestParseCloudInitCustom(t *testing.T) {
	custom := parseCloudInitCustom("user=local:snippets/user.yaml,network=local:snippets/net.yaml")
	if len(custom) != 2 || custom[0].Device != "user" || custom[1].Volume != "local:snippets/net.yaml" {
		t.Fatalf("custom = %#v", custom)
	}
	if got := parseCloudInitCustom(""); got != nil {
		t.Fatalf("empty custom = %#v", got)
	}
	if got := parseCloudInitCustom("malformed"); got != nil {
		t.Fatalf("malformed custom = %#v", got)
	}
}

func TestVirtualMachineCloudInit(t *testing.T) {
	backend := &fakeBackend{
		cloudInitConfigs: map[int]output.CloudInitConfig{
			100: {
				VMID:               100,
				Node:               "pve1",
				User:               "debian",
				PasswordConfigured: true,
				IPConfigs:          []output.CloudInitIPConfig{{Device: "ipconfig0", Config: "ip=dhcp"}},
				Type:               "nocloud",
			},
		},
	}

	config, err := backend.VirtualMachineCloudInit(context.Background(), "pve1", 100)
	if err != nil {
		t.Fatalf("cloud init: %v", err)
	}
	if config.User != "debian" || !config.PasswordConfigured {
		t.Fatalf("config = %#v", config)
	}
	if _, err := backend.VirtualMachineCloudInit(context.Background(), "pve1", 404); err == nil {
		t.Fatal("expected not found error")
	}
}
