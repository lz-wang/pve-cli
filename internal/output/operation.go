package output

import (
	"fmt"
	"io"
)

// OperationResult is the v3 mutation result. Unknown never means cancelled.
type OperationResult struct {
	Resource string `json:"resource" yaml:"resource"`
	VMID     int    `json:"vmid,omitempty" yaml:"vmid,omitempty"`
	Node     string `json:"node,omitempty" yaml:"node,omitempty"`
	Task     string `json:"task,omitempty" yaml:"task,omitempty"`
	Status   string `json:"status" yaml:"status"`
	Error    string `json:"error,omitempty" yaml:"error,omitempty"`
}

func WriteOperation(w io.Writer, format string, r OperationResult) error {
	return Write(w, format, r, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "RESOURCE\tVMID\tNODE\tSTATUS\tTASK\n%s\t%d\t%s\t%s\t%s\n", r.Resource, r.VMID, r.Node, r.Status, r.Task)
		return err
	})
}
