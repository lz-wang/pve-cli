package pve

import (
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"sort"
	"strings"
)

// QueryGuests applies stable filters and sorting in the service layer.
func QueryGuests(rows []output.GuestRow, name, status, key string, desc bool) ([]output.GuestRow, error) {
	if key == "" {
		key = "vmid"
	}
	switch key {
	case "vmid", "name", "node", "status", "memory", "cpu":
	default:
		return nil, fmt.Errorf("%w: invalid guest sort %q", ErrInvalid, key)
	}
	result := make([]output.GuestRow, 0, len(rows))
	for _, r := range rows {
		if name != "" && !strings.Contains(strings.ToLower(r.Name), strings.ToLower(name)) {
			continue
		}
		if status != "" && !strings.EqualFold(status, r.Status) {
			continue
		}
		result = append(result, r)
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		missingA, missingB := false, false
		cmp := 0
		switch key {
		case "name":
			cmp = strings.Compare(a.Name, b.Name)
		case "node":
			cmp = strings.Compare(a.Node, b.Node)
		case "status":
			cmp = strings.Compare(a.Status, b.Status)
		case "memory":
			missingA = a.MaxMem == 0
			missingB = b.MaxMem == 0
			if a.MaxMem < b.MaxMem {
				cmp = -1
			} else if a.MaxMem > b.MaxMem {
				cmp = 1
			}
		case "cpu":
			missingA = a.CPUs == 0
			missingB = b.CPUs == 0
			if a.CPU < b.CPU {
				cmp = -1
			} else if a.CPU > b.CPU {
				cmp = 1
			}
		default:
			if a.VMID < b.VMID {
				cmp = -1
			} else if a.VMID > b.VMID {
				cmp = 1
			}
		}
		if missingA != missingB {
			return !missingA
		}
		if cmp == 0 {
			if a.VMID != b.VMID {
				return a.VMID < b.VMID
			}
			return a.Node < b.Node
		}
		if desc {
			return cmp > 0
		}
		return cmp < 0
	})
	return result, nil
}
