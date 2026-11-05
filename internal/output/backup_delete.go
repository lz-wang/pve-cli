package output

import (
	"fmt"
	"io"
)

type BackupDeleteResult struct {
	OperationResult `yaml:",inline"`
	Archive         string `json:"archive" yaml:"archive"`
}

func WriteBackupDelete(w io.Writer, format string, r BackupDeleteResult) error {
	return Write(w, format, r, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "NODE\tARCHIVE\tSTATUS\tTASK\n%s\t%s\t%s\t%s\n", r.Node, r.Archive, r.Status, r.Task)
		return err
	})
}
