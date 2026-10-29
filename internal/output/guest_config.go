package output

import (
	"fmt"
	"io"
	"sort"
)

type GuestConfig struct {
	Resource string         `json:"resource" yaml:"resource"`
	VMID     int            `json:"vmid" yaml:"vmid"`
	Node     string         `json:"node" yaml:"node"`
	Config   map[string]any `json:"config" yaml:"config"`
}

func WriteGuestConfig(w io.Writer, format string, result GuestConfig) error {
	return Write(w, format, result, func(w io.Writer) error {
		keys := make([]string, 0, len(result.Config))
		for k := range result.Config {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if _, err := fmt.Fprintf(w, "%s\t%v\n", k, result.Config[k]); err != nil {
				return err
			}
		}
		return nil
	})
}
