package output

import (
	"fmt"
	"io"
)

// TemplateRow distinguishes downloadable templates from local volumes.
type TemplateRow struct {
	Name      string `json:"name" yaml:"name"`
	Section   string `json:"section" yaml:"section"`
	Size      uint64 `json:"size" yaml:"size"`
	Node      string `json:"node" yaml:"node"`
	Storage   string `json:"storage,omitempty" yaml:"storage,omitempty"`
	VolID     string `json:"volid,omitempty" yaml:"volid,omitempty"`
	Available bool   `json:"available" yaml:"available"`
}

func WriteTemplates(w io.Writer, format string, rows []TemplateRow) error {
	return Write(w, format, rows, func(w io.Writer) error {
		if _, err := fmt.Fprintln(w, "NAME\tSECTION\tSIZE\tNODE\tSTORAGE\tAVAILABLE"); err != nil {
			return err
		}
		for _, r := range rows {
			if _, err := fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%t\n", r.Name, r.Section, r.Size, r.Node, r.Storage, r.Available); err != nil {
				return err
			}
		}
		return nil
	})
}
