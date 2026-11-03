package output

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Migration check statuses.
const (
	MigrationCheckOK      = "ok"
	MigrationCheckWarning = "warning"
	MigrationCheckUnknown = "unknown"
	MigrationCheckFail    = "fail"
)

// MigrationCheckRow is one migration preflight or post-migration finding.
type MigrationCheckRow struct {
	Check   string `json:"check" yaml:"check"`
	Status  string `json:"status" yaml:"status"`
	Message string `json:"message" yaml:"message"`
}

// MigrationPlan aggregates the read-only cross-profile migration checks.
// Status is "failed" when any check failed, "warning" when checks only
// warned or stayed unknown, and "ok" otherwise.
type MigrationPlan struct {
	Resource   string              `json:"resource" yaml:"resource"`
	VMID       int                 `json:"vmid" yaml:"vmid"`
	Node       string              `json:"node" yaml:"node"`
	TargetNode string              `json:"target_node,omitempty" yaml:"target_node,omitempty"`
	TargetVMID int                 `json:"target_vmid,omitempty" yaml:"target_vmid,omitempty"`
	Status     string              `json:"status" yaml:"status"`
	Checks     []MigrationCheckRow `json:"checks" yaml:"checks"`
}

func WriteMigrationPlan(w io.Writer, format string, plan MigrationPlan) error {
	return Write(w, format, plan, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintf(tw, "PLAN\t%s %d -> %s:%d\t%s\n", plan.Resource, plan.VMID, plan.TargetNode, plan.TargetVMID, plan.Status); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(tw, "STATUS\tCHECK\tMESSAGE"); err != nil {
			return err
		}
		for _, row := range plan.Checks {
			if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\n", row.Status, row.Check, strings.TrimSpace(row.Message)); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}
