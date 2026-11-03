package output

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// BackupCopyResult reports one cross-profile backup archive transfer.
type BackupCopyResult struct {
	Resource      string `json:"resource" yaml:"resource"`
	Status        string `json:"status" yaml:"status"`
	Error         string `json:"error,omitempty" yaml:"error,omitempty"`
	SourceVolid   string `json:"source_volid" yaml:"source_volid"`
	TargetStorage string `json:"target_storage" yaml:"target_storage"`
	TargetPath    string `json:"target_path" yaml:"target_path"`
	Bytes         uint64 `json:"bytes" yaml:"bytes"`
	SHA256        string `json:"sha256,omitempty" yaml:"sha256,omitempty"`
	Resumed       bool   `json:"resumed,omitempty" yaml:"resumed,omitempty"`
}

func WriteBackupCopyResult(w io.Writer, format string, result BackupCopyResult) error {
	return Write(w, format, result, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "SOURCE\tTARGET\tBYTES\tSHA256\tRESUMED\tSTATUS"); err != nil {
			return err
		}
		sha := result.SHA256
		if len(sha) > 12 {
			sha = sha[:12] + "…"
		}
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\n",
			result.SourceVolid,
			strings.TrimSpace(result.TargetPath),
			FormatBytes(result.Bytes),
			empty(sha),
			formatBool(result.Resumed),
			result.Status,
		); err != nil {
			return err
		}
		if result.Error != "" {
			if _, err := fmt.Fprintf(tw, "ERROR\t%s\n", result.Error); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}
