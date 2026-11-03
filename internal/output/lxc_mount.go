package output

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// LXCMountResult reports one offline mount/unmount operation on the host.
type LXCMountResult struct {
	Resource string `json:"resource" yaml:"resource"`
	VMID     int    `json:"vmid" yaml:"vmid"`
	Node     string `json:"node" yaml:"node"`
	Status   string `json:"status" yaml:"status"`
	Mounted  bool   `json:"mounted" yaml:"mounted"`
	Path     string `json:"path,omitempty" yaml:"path,omitempty"`
	Message  string `json:"message,omitempty" yaml:"message,omitempty"`
}

func WriteLXCMountResult(w io.Writer, format string, result LXCMountResult) error {
	return Write(w, format, result, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "VMID\tNODE\tMOUNTED\tPATH\tSTATUS\tMESSAGE"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(
			tw,
			"%d\t%s\t%s\t%s\t%s\t%s\n",
			result.VMID,
			result.Node,
			formatBool(result.Mounted),
			empty(result.Path),
			result.Status,
			result.Message,
		); err != nil {
			return err
		}
		return tw.Flush()
	})
}
