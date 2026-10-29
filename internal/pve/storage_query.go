package pve

import (
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"math"
	"sort"
	"strings"
)

func queryStorages(rows []output.StorageRow, o StorageListOptions) ([]output.StorageRow, error) {
	if math.IsNaN(o.MinUsedPercent) || o.MinUsedPercent < 0 || o.MinUsedPercent > 100 {
		return nil, fmt.Errorf("%w: min-used-percent must be 0..100", ErrInvalid)
	}
	switch o.Sort {
	case "", "name", "node", "used-percent", "total":
	default:
		return nil, fmt.Errorf("%w: invalid storage sort", ErrInvalid)
	}
	result := make([]output.StorageRow, 0, len(rows))
	for _, r := range filterStorageRows(rows, o) {
		if o.MinUsedPercent > 0 && (r.Total == 0 || r.UsedFraction*100 < o.MinUsedPercent) {
			continue
		}
		result = append(result, r)
	}
	if o.Sort == "" {
		return sortStorageRows(result), nil
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if (o.Sort == "used-percent" || o.Sort == "total") && (a.Total == 0) != (b.Total == 0) {
			return a.Total != 0
		}
		cmp := 0
		switch o.Sort {
		case "name":
			cmp = strings.Compare(a.Storage, b.Storage)
		case "node":
			cmp = strings.Compare(a.Node, b.Node)
		case "used-percent":
			if a.UsedFraction < b.UsedFraction {
				cmp = -1
			} else if a.UsedFraction > b.UsedFraction {
				cmp = 1
			}
		case "total":
			if a.Total < b.Total {
				cmp = -1
			} else if a.Total > b.Total {
				cmp = 1
			}
		}
		if cmp == 0 {
			if a.Node != b.Node {
				return a.Node < b.Node
			}
			return a.Storage < b.Storage
		}
		if o.Desc {
			return cmp > 0
		}
		return cmp < 0
	})
	return result, nil
}
