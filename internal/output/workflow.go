package output

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// WorkflowStageResult is one stage of a composed workflow. Status is one of
// succeeded, skipped, failed, partial, warning, unknown — every stage keeps
// its own result and error state, independent of the others. partial means
// the stage's task completed while its post-step failed; warning means the
// stage completed with unconfirmed risks that are not observable via the
// API.
type WorkflowStageResult struct {
	Stage  string `json:"stage" yaml:"stage"`
	Status string `json:"status" yaml:"status"`
	Task   string `json:"task,omitempty" yaml:"task,omitempty"`
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// WorkflowResult reports a backup → transfer → verify → restore →
// post-check pipeline.
type WorkflowResult struct {
	Resource   string                `json:"resource" yaml:"resource"`
	VMID       int                   `json:"vmid" yaml:"vmid"`
	TargetVMID int                   `json:"target_vmid" yaml:"target_vmid"`
	TargetNode string                `json:"target_node,omitempty" yaml:"target_node,omitempty"`
	Archive    string                `json:"archive,omitempty" yaml:"archive,omitempty"`
	Status     string                `json:"status" yaml:"status"`
	Stages     []WorkflowStageResult `json:"stages" yaml:"stages"`
}

func WriteWorkflowResult(w io.Writer, format string, result WorkflowResult) error {
	return Write(w, format, result, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "STAGE\tSTATUS\tTASK\tDETAIL"); err != nil {
			return err
		}
		for _, stage := range result.Stages {
			if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", stage.Stage, stage.Status, empty(stage.Task), empty(stage.Detail)); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}
