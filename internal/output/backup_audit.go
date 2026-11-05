package output

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

type BackupAuditReport struct {
	Complete bool             `json:"complete" yaml:"complete"`
	Rows     []BackupAuditRow `json:"rows" yaml:"rows"`
	Errors   []string         `json:"errors" yaml:"errors"`
}
type BackupAuditRow struct {
	Node            string      `json:"node" yaml:"node"`
	Kind            string      `json:"kind" yaml:"kind"`
	VMID            uint64      `json:"vmid" yaml:"vmid"`
	Name            string      `json:"name" yaml:"name"`
	Status          string      `json:"status" yaml:"status"`
	LatestCTime     uint64      `json:"latest_ctime,omitempty" yaml:"latest_ctime,omitempty"`
	Archives        []BackupRow `json:"archives" yaml:"archives"`
	Association     string      `json:"association" yaml:"association"`
	OffHostCopy     string      `json:"off_host_copy" yaml:"off_host_copy"`
	RestoreVerified string      `json:"restore_verified" yaml:"restore_verified"`
	Findings        []string    `json:"findings" yaml:"findings"`
}

func WriteBackupAudit(w io.Writer, format string, r BackupAuditReport) error {
	return Write(w, format, r, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NODE\tKIND\tVMID\tNAME\tARCHIVES\tSTATUS\tFINDINGS")
		for _, row := range r.Rows {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%d\t%s\t%s\n", row.Node, row.Kind, row.VMID, row.Name, len(row.Archives), row.Status, strings.Join(row.Findings, "; "))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		for _, e := range r.Errors {
			if _, err := fmt.Fprintln(w, "UNKNOWN:", e); err != nil {
				return err
			}
		}
		return nil
	})
}
