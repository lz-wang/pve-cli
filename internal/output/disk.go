package output

import (
	"fmt"
	"io"
)

type DiskRow struct {
	VMID   int    `json:"vmid" yaml:"vmid"`
	Node   string `json:"node" yaml:"node"`
	Slot   string `json:"slot" yaml:"slot"`
	Volume string `json:"volume" yaml:"volume"`
	Device string `json:"device" yaml:"device"`
	Size   string `json:"size,omitempty" yaml:"size,omitempty"`
	Role   string `json:"role" yaml:"role"`
}
type DiskResult struct {
	OperationResult `yaml:",inline"`
	Slot            string `json:"slot" yaml:"slot"`
	Volume          string `json:"volume,omitempty" yaml:"volume,omitempty"`
}

func WriteDisks(w io.Writer, format string, rows []DiskRow) error {
	return Write(w, format, rows, func(w io.Writer) error {
		if _, err := fmt.Fprintln(w, "SLOT\tVOLUME\tDEVICE\tSIZE\tROLE"); err != nil {
			return err
		}
		for _, r := range rows {
			if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Slot, r.Volume, r.Device, r.Size, r.Role); err != nil {
				return err
			}
		}
		return nil
	})
}
func WriteDiskResult(w io.Writer, format string, r DiskResult) error {
	return Write(w, format, r, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "%s %s: %s (%s)\n", r.Resource, r.Slot, r.Status, r.Volume)
		return err
	})
}
