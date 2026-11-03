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
	// Verified reports the post-write state check for configuration
	// mutations. nil means the outcome was not verified (submitted async,
	// unverifiable value, or no verification backend); false means the
	// re-read state differs from the requested values.
	Verified *bool `json:"verified,omitempty" yaml:"verified,omitempty"`
	// APIError preserves the original sanitized API failure when a write
	// errored but the follow-up state check confirmed the requested values.
	APIError string `json:"api_error,omitempty" yaml:"api_error,omitempty"`
}

func WriteOperation(w io.Writer, format string, r OperationResult) error {
	return Write(w, format, r, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "RESOURCE\tVMID\tNODE\tSTATUS\tTASK\n%s\t%d\t%s\t%s\t%s\n", r.Resource, r.VMID, r.Node, r.Status, r.Task)
		return err
	})
}
