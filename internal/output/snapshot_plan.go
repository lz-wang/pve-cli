package output

import (
	"fmt"
	"io"
)

type SnapshotPlan struct {
	Resource string `json:"resource" yaml:"resource"`
	VMID     int    `json:"vmid" yaml:"vmid"`
	Node     string `json:"node" yaml:"node"`
	Snapshot string `json:"snapshot" yaml:"snapshot"`
	Action   string `json:"action" yaml:"action"`
	Impact   string `json:"impact" yaml:"impact"`
}

func WriteSnapshotPlan(w io.Writer, format string, r SnapshotPlan) error {
	return Write(w, format, r, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "DRY-RUN: %s %s %d on %s snapshot %s: %s\n", r.Action, r.Resource, r.VMID, r.Node, r.Snapshot, r.Impact)
		return err
	})
}
