package pve

import (
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"testing"
)

func TestStorageQueryKeepsUnknownLast(t *testing.T) {
	rows := []output.StorageRow{{Storage: "unknown"}, {Storage: "low", Total: 100, UsedFraction: .5}, {Storage: "high", Total: 100, UsedFraction: .9}}
	got, e := queryStorages(rows, StorageListOptions{Sort: "used-percent", Desc: true})
	if e != nil || got[0].Storage != "high" || got[2].Storage != "unknown" {
		t.Fatalf("%+v %v", got, e)
	}
	got, e = queryStorages(rows, StorageListOptions{MinUsedPercent: 80})
	if e != nil || len(got) != 1 || got[0].Storage != "high" {
		t.Fatalf("%+v %v", got, e)
	}
}
