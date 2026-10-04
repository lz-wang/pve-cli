package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	FormatTable = "table"
	FormatJSON  = "json"
	FormatYAML  = "yaml"
)

type DoctorStatus string

const (
	DoctorStatusOK   DoctorStatus = "ok"
	DoctorStatusWarn DoctorStatus = "warn"
	DoctorStatusFail DoctorStatus = "fail"
	DoctorStatusSkip DoctorStatus = "skip"
)

type NodeRow struct {
	Name    string  `json:"name" yaml:"name"`
	Status  string  `json:"status" yaml:"status"`
	CPU     float64 `json:"cpu" yaml:"cpu"`
	Mem     uint64  `json:"mem" yaml:"mem"`
	MaxMem  uint64  `json:"max_mem" yaml:"max_mem"`
	Disk    uint64  `json:"disk" yaml:"disk"`
	MaxDisk uint64  `json:"max_disk" yaml:"max_disk"`
	Uptime  uint64  `json:"uptime" yaml:"uptime"`
}

type GuestRow struct {
	Kind    string  `json:"kind" yaml:"kind"`
	VMID    uint64  `json:"vmid" yaml:"vmid"`
	Name    string  `json:"name" yaml:"name"`
	Node    string  `json:"node" yaml:"node"`
	Status  string  `json:"status" yaml:"status"`
	CPUs    int     `json:"cpus" yaml:"cpus"`
	CPU     float64 `json:"cpu" yaml:"cpu"`
	Mem     uint64  `json:"mem" yaml:"mem"`
	MaxMem  uint64  `json:"max_mem" yaml:"max_mem"`
	MaxDisk uint64  `json:"max_disk" yaml:"max_disk"`
	Uptime  uint64  `json:"uptime" yaml:"uptime"`
	Tags    string  `json:"tags,omitempty" yaml:"tags,omitempty"`
}

type CloneResult struct {
	Kind       string `json:"kind" yaml:"kind"`
	SourceVMID uint64 `json:"source_vmid" yaml:"source_vmid"`
	NewVMID    uint64 `json:"new_vmid" yaml:"new_vmid"`
	SourceNode string `json:"source_node" yaml:"source_node"`
	TargetNode string `json:"target_node" yaml:"target_node"`
	Name       string `json:"name" yaml:"name"`
	Task       string `json:"task,omitempty" yaml:"task,omitempty"`
}

type SnapshotRow struct {
	Kind        string `json:"kind" yaml:"kind"`
	VMID        uint64 `json:"vmid" yaml:"vmid"`
	Node        string `json:"node" yaml:"node"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Parent      string `json:"parent" yaml:"parent"`
	Snaptime    int64  `json:"snaptime" yaml:"snaptime"`
	VMState     int    `json:"vmstate" yaml:"vmstate"`
	State       string `json:"state" yaml:"state"`
}

type BackupRow struct {
	Node        string `json:"node" yaml:"node"`
	Storage     string `json:"storage" yaml:"storage"`
	Kind        string `json:"kind" yaml:"kind"`
	VMID        uint64 `json:"vmid" yaml:"vmid"`
	VolID       string `json:"volid" yaml:"volid"`
	Format      string `json:"format" yaml:"format"`
	Size        uint64 `json:"size" yaml:"size"`
	Used        uint64 `json:"used,omitempty" yaml:"used,omitempty"`
	CTime       uint64 `json:"ctime" yaml:"ctime"`
	Protected   string `json:"protected,omitempty" yaml:"protected,omitempty"`
	Encrypted   string `json:"encrypted,omitempty" yaml:"encrypted,omitempty"`
	VerifyState string `json:"verify_state,omitempty" yaml:"verify_state,omitempty"`
	Notes       string `json:"notes,omitempty" yaml:"notes,omitempty"`
}

type BackupResult struct {
	Kind    string `json:"kind" yaml:"kind"`
	VMID    uint64 `json:"vmid" yaml:"vmid"`
	Node    string `json:"node" yaml:"node"`
	Storage string `json:"storage" yaml:"storage"`
	Mode    string `json:"mode" yaml:"mode"`
	Task    string `json:"task,omitempty" yaml:"task,omitempty"`
}

type RestoreResult struct {
	Kind    string `json:"kind" yaml:"kind"`
	VMID    uint64 `json:"vmid" yaml:"vmid"`
	Node    string `json:"node" yaml:"node"`
	Archive string `json:"archive" yaml:"archive"`
	Storage string `json:"storage,omitempty" yaml:"storage,omitempty"`
	Task    string `json:"task,omitempty" yaml:"task,omitempty"`
}

type StatusReport struct {
	Nodes    NodeSummary    `json:"nodes" yaml:"nodes"`
	Guests   GuestSummary   `json:"guests" yaml:"guests"`
	Storages StorageSummary `json:"storages" yaml:"storages"`
	Backups  BackupSummary  `json:"backups" yaml:"backups"`
	Issues   []StatusIssue  `json:"issues,omitempty" yaml:"issues,omitempty"`
}

type NodeSummary struct {
	Total   int       `json:"total" yaml:"total"`
	Online  int       `json:"online" yaml:"online"`
	Offline int       `json:"offline" yaml:"offline"`
	Rows    []NodeRow `json:"rows,omitempty" yaml:"rows,omitempty"`
}

type GuestSummary struct {
	Total   int `json:"total" yaml:"total"`
	Running int `json:"running" yaml:"running"`
	Stopped int `json:"stopped" yaml:"stopped"`
	VM      int `json:"vm" yaml:"vm"`
	LXC     int `json:"lxc" yaml:"lxc"`
}

type StorageSummary struct {
	Total  int          `json:"total" yaml:"total"`
	Active int          `json:"active" yaml:"active"`
	Rows   []StorageRow `json:"rows,omitempty" yaml:"rows,omitempty"`
}

type BackupSummary struct {
	Count       int    `json:"count" yaml:"count"`
	LatestCtime uint64 `json:"latest_ctime,omitempty" yaml:"latest_ctime,omitempty"`
}

type StatusIssue struct {
	Component string `json:"component" yaml:"component"`
	Message   string `json:"message" yaml:"message"`
}

type StorageRow struct {
	Node         string  `json:"node" yaml:"node"`
	Storage      string  `json:"storage" yaml:"storage"`
	Type         string  `json:"type" yaml:"type"`
	Active       bool    `json:"active" yaml:"active"`
	Enabled      bool    `json:"enabled" yaml:"enabled"`
	Shared       bool    `json:"shared" yaml:"shared"`
	Content      string  `json:"content" yaml:"content"`
	Used         uint64  `json:"used" yaml:"used"`
	Avail        uint64  `json:"avail" yaml:"avail"`
	Total        uint64  `json:"total" yaml:"total"`
	UsedFraction float64 `json:"used_fraction" yaml:"used_fraction"`
}

type StorageContentRow struct {
	Node        string `json:"node" yaml:"node"`
	Storage     string `json:"storage" yaml:"storage"`
	Content     string `json:"content" yaml:"content"`
	VMID        uint64 `json:"vmid,omitempty" yaml:"vmid,omitempty"`
	VolID       string `json:"volid" yaml:"volid"`
	Format      string `json:"format,omitempty" yaml:"format,omitempty"`
	Size        uint64 `json:"size" yaml:"size"`
	Used        uint64 `json:"used,omitempty" yaml:"used,omitempty"`
	CTime       uint64 `json:"ctime,omitempty" yaml:"ctime,omitempty"`
	Protected   string `json:"protected,omitempty" yaml:"protected,omitempty"`
	Encrypted   string `json:"encrypted,omitempty" yaml:"encrypted,omitempty"`
	VerifyState string `json:"verify_state,omitempty" yaml:"verify_state,omitempty"`
	Notes       string `json:"notes,omitempty" yaml:"notes,omitempty"`
}

type DoctorRow struct {
	Check   string       `json:"check" yaml:"check"`
	Status  DoctorStatus `json:"status" yaml:"status"`
	Message string       `json:"message" yaml:"message"`
}

type TaskRow struct {
	UPID       string `json:"upid" yaml:"upid"`
	Node       string `json:"node" yaml:"node"`
	Type       string `json:"type" yaml:"type"`
	ID         string `json:"id,omitempty" yaml:"id,omitempty"`
	User       string `json:"user,omitempty" yaml:"user,omitempty"`
	Status     string `json:"status" yaml:"status"`
	ExitStatus string `json:"exit_status,omitempty" yaml:"exit_status,omitempty"`
	StartTime  int64  `json:"start_time,omitempty" yaml:"start_time,omitempty"`
	EndTime    int64  `json:"end_time,omitempty" yaml:"end_time,omitempty"`
}

type TaskLogRow struct {
	Line int    `json:"line" yaml:"line"`
	Text string `json:"text" yaml:"text"`
}

type VersionInfo struct {
	Version   string `json:"version" yaml:"version"`
	Commit    string `json:"commit" yaml:"commit"`
	Date      string `json:"date" yaml:"date"`
	GoVersion string `json:"go_version" yaml:"go_version"`
	OS        string `json:"os" yaml:"os"`
	Arch      string `json:"arch" yaml:"arch"`
}

func ValidateFormat(format string) error {
	switch strings.ToLower(format) {
	case FormatTable, FormatJSON, FormatYAML:
		return nil
	default:
		return fmt.Errorf("invalid output format %q, expected table, json, or yaml", format)
	}
}

func NormalizeFormat(format string) string {
	if format == "" {
		return FormatTable
	}
	return strings.ToLower(format)
}

func Write(w io.Writer, format string, value any, writeTable func(io.Writer) error) error {
	format = NormalizeFormat(format)
	if err := ValidateFormat(format); err != nil {
		return err
	}

	switch format {
	case FormatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	case FormatYAML:
		data, err := yaml.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode yaml: %w", err)
		}
		_, err = w.Write(data)
		return err
	case FormatTable:
		if writeTable == nil {
			return errors.New("table renderer is required")
		}
		return writeTable(w)
	default:
		return nil
	}
}

func WriteNodeRows(w io.Writer, format string, rows []NodeRow) error {
	return Write(w, format, rows, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "NAME\tSTATUS\tCPU\tMEM\tMAXMEM\tDISK\tMAXDISK\tUPTIME"); err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := fmt.Fprintf(
				tw,
				"%s\t%s\t%.2f\t%s\t%s\t%s\t%s\t%s\n",
				row.Name,
				row.Status,
				row.CPU,
				FormatBytes(row.Mem),
				FormatBytes(row.MaxMem),
				FormatBytes(row.Disk),
				FormatBytes(row.MaxDisk),
				FormatUptime(row.Uptime),
			); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}

func WriteDoctorRows(w io.Writer, format string, rows []DoctorRow) error {
	return Write(w, format, rows, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "CHECK\tSTATUS\tMESSAGE"); err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\n", row.Check, row.Status, row.Message); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}

func WriteTaskRows(w io.Writer, format string, rows []TaskRow) error {
	return Write(w, format, rows, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "NODE\tTYPE\tID\tUSER\tSTATUS\tSTARTTIME\tENDTIME\tEXITSTATUS\tUPID"); err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := fmt.Fprintf(
				tw,
				"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				empty(row.Node),
				empty(row.Type),
				empty(row.ID),
				empty(row.User),
				empty(row.Status),
				formatUnixTime(row.StartTime),
				formatUnixTime(row.EndTime),
				empty(row.ExitStatus),
				row.UPID,
			); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}

func WriteTaskDetail(w io.Writer, format string, row TaskRow) error {
	return Write(w, format, row, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		lines := [][2]string{
			{"UPID", row.UPID},
			{"Node", empty(row.Node)},
			{"Type", empty(row.Type)},
			{"ID", empty(row.ID)},
			{"User", empty(row.User)},
			{"Status", empty(row.Status)},
			{"ExitStatus", empty(row.ExitStatus)},
			{"StartTime", formatUnixTime(row.StartTime)},
			{"EndTime", formatUnixTime(row.EndTime)},
		}
		for _, line := range lines {
			if _, err := fmt.Fprintf(tw, "%s:\t%s\n", line[0], line[1]); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}

func WriteTaskLogRows(w io.Writer, format string, rows []TaskLogRow) error {
	return Write(w, format, rows, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, row := range rows {
			if _, err := fmt.Fprintf(tw, "%d\t%s\n", row.Line, row.Text); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}

func WriteVersionInfo(w io.Writer, format string, info VersionInfo) error {
	return Write(w, format, info, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "VERSION\tCOMMIT\tDATE\tGO\tOS\tARCH"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\n",
			info.Version,
			info.Commit,
			info.Date,
			info.GoVersion,
			info.OS,
			info.Arch,
		); err != nil {
			return err
		}
		return tw.Flush()
	})
}

func WriteGuestRows(w io.Writer, format string, rows []GuestRow) error {
	return Write(w, format, rows, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "VMID\tNAME\tNODE\tSTATUS\tCPUS\tMEM\tMAXMEM\tMAXDISK\tUPTIME\tTAGS"); err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := fmt.Fprintf(
				tw,
				"%d\t%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n",
				row.VMID,
				empty(row.Name),
				row.Node,
				row.Status,
				row.CPUs,
				FormatBytes(row.Mem),
				FormatBytes(row.MaxMem),
				FormatBytes(row.MaxDisk),
				FormatUptime(row.Uptime),
				empty(row.Tags),
			); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}

func WriteGuestRowsWithKind(w io.Writer, format string, rows []GuestRow) error {
	return Write(w, format, rows, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "KIND\tVMID\tNAME\tNODE\tSTATUS\tCPUS\tMEM\tMAXMEM\tMAXDISK\tUPTIME\tTAGS"); err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := fmt.Fprintf(
				tw,
				"%s\t%d\t%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n",
				row.Kind,
				row.VMID,
				empty(row.Name),
				row.Node,
				row.Status,
				row.CPUs,
				FormatBytes(row.Mem),
				FormatBytes(row.MaxMem),
				FormatBytes(row.MaxDisk),
				FormatUptime(row.Uptime),
				empty(row.Tags),
			); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}

func WriteGuestDetail(w io.Writer, format string, row GuestRow) error {
	return Write(w, format, row, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		lines := [][2]string{
			{"Kind", row.Kind},
			{"VMID", fmt.Sprint(row.VMID)},
			{"Name", empty(row.Name)},
			{"Node", row.Node},
			{"Status", row.Status},
			{"CPUs", fmt.Sprint(row.CPUs)},
			{"CPU", fmt.Sprintf("%.2f", row.CPU)},
			{"Memory", FormatBytes(row.Mem)},
			{"Max Memory", FormatBytes(row.MaxMem)},
			{"Max Disk", FormatBytes(row.MaxDisk)},
			{"Uptime", FormatUptime(row.Uptime)},
			{"Tags", empty(row.Tags)},
		}
		for _, line := range lines {
			if _, err := fmt.Fprintf(tw, "%s:\t%s\n", line[0], line[1]); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}

func WriteCloneResult(w io.Writer, format string, result CloneResult) error {
	return Write(w, format, result, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "KIND\tSOURCE\tNEWID\tSOURCE_NODE\tTARGET_NODE\tNAME\tTASK"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%d\t%d\t%s\t%s\t%s\t%s\n",
			result.Kind,
			result.SourceVMID,
			result.NewVMID,
			result.SourceNode,
			result.TargetNode,
			empty(result.Name),
			empty(result.Task),
		); err != nil {
			return err
		}
		return tw.Flush()
	})
}

func WriteSnapshotRows(w io.Writer, format string, rows []SnapshotRow) error {
	return Write(w, format, rows, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "KIND\tNAME\tVMID\tNODE\tPARENT\tSNAPTIME\tVMSTATE\tSTATE\tDESCRIPTION"); err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := fmt.Fprintf(
				tw,
				"%s\t%s\t%d\t%s\t%s\t%s\t%d\t%s\t%s\n",
				row.Kind,
				row.Name,
				row.VMID,
				row.Node,
				empty(row.Parent),
				formatUnixTime(row.Snaptime),
				row.VMState,
				empty(row.State),
				empty(row.Description),
			); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}

func WriteBackupRows(w io.Writer, format string, rows []BackupRow) error {
	return Write(w, format, rows, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "NODE\tSTORAGE\tKIND\tVMID\tFORMAT\tSIZE\tCTIME\tPROTECTED\tVERIFY\tVOLID"); err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := fmt.Fprintf(
				tw,
				"%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
				row.Node,
				row.Storage,
				row.Kind,
				row.VMID,
				empty(row.Format),
				FormatBytes(row.Size),
				formatUnixTime(int64(row.CTime)),
				empty(row.Protected),
				empty(row.VerifyState),
				row.VolID,
			); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}

func WriteBackupResult(w io.Writer, format string, result BackupResult) error {
	return Write(w, format, result, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "KIND\tVMID\tNODE\tSTORAGE\tMODE\tTASK"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%d\t%s\t%s\t%s\t%s\n",
			result.Kind,
			result.VMID,
			result.Node,
			result.Storage,
			result.Mode,
			empty(result.Task),
		); err != nil {
			return err
		}
		return tw.Flush()
	})
}

func WriteRestoreResult(w io.Writer, format string, result RestoreResult) error {
	return Write(w, format, result, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "KIND\tVMID\tNODE\tSTORAGE\tARCHIVE\tTASK"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%d\t%s\t%s\t%s\t%s\n",
			result.Kind,
			result.VMID,
			result.Node,
			empty(result.Storage),
			result.Archive,
			empty(result.Task),
		); err != nil {
			return err
		}
		return tw.Flush()
	})
}

func WriteStorageRows(w io.Writer, format string, rows []StorageRow) error {
	return Write(w, format, rows, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "NODE\tSTORAGE\tTYPE\tACTIVE\tENABLED\tSHARED\tCONTENT\tUSED\tAVAIL\tTOTAL\tUSED%"); err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := fmt.Fprintf(
				tw,
				"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%.1f\n",
				row.Node,
				row.Storage,
				empty(row.Type),
				formatBool(row.Active),
				formatBool(row.Enabled),
				formatBool(row.Shared),
				empty(row.Content),
				FormatBytes(row.Used),
				FormatBytes(row.Avail),
				FormatBytes(row.Total),
				row.UsedFraction*100,
			); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}

func WriteStorageDetail(w io.Writer, format string, row StorageRow) error {
	return Write(w, format, row, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		lines := [][2]string{
			{"Node", row.Node},
			{"Storage", row.Storage},
			{"Type", empty(row.Type)},
			{"Active", formatBool(row.Active)},
			{"Enabled", formatBool(row.Enabled)},
			{"Shared", formatBool(row.Shared)},
			{"Content", empty(row.Content)},
			{"Used", FormatBytes(row.Used)},
			{"Avail", FormatBytes(row.Avail)},
			{"Total", FormatBytes(row.Total)},
			{"Used%", fmt.Sprintf("%.1f", row.UsedFraction*100)},
		}
		for _, line := range lines {
			if _, err := fmt.Fprintf(tw, "%s:\t%s\n", line[0], line[1]); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}

func WriteStorageContentRows(w io.Writer, format string, rows []StorageContentRow) error {
	return Write(w, format, rows, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "NODE\tSTORAGE\tCONTENT\tVMID\tFORMAT\tSIZE\tUSED\tCTIME\tPROTECTED\tENCRYPTED\tVERIFY\tVOLID"); err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := fmt.Fprintf(
				tw,
				"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				row.Node,
				row.Storage,
				empty(row.Content),
				formatOptionalUint(row.VMID),
				empty(row.Format),
				FormatBytes(row.Size),
				formatOptionalBytes(row.Used),
				formatUnixTime(int64(row.CTime)),
				empty(row.Protected),
				empty(row.Encrypted),
				empty(row.VerifyState),
				row.VolID,
			); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}

// WriteBulkPlan renders a bulk-operation selection. Structured output reuses
// GuestRow; the table view shows only the identifying columns.
func WriteBulkPlan(w io.Writer, format string, rows []GuestRow) error {
	return Write(w, format, rows, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		if _, err := fmt.Fprintln(tw, "KIND\tVMID\tNODE\tNAME\tSTATUS"); err != nil {
			return err
		}
		for _, row := range rows {
			if _, err := fmt.Fprintf(
				tw,
				"%s\t%d\t%s\t%s\t%s\n",
				row.Kind,
				row.VMID,
				row.Node,
				empty(row.Name),
				empty(row.Status),
			); err != nil {
				return err
			}
		}
		return tw.Flush()
	})
}

func WriteStatusReport(w io.Writer, format string, report StatusReport) error {
	return Write(w, format, report, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

		if _, err := fmt.Fprintln(tw, "NODE"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(tw, "NAME\tSTATUS\tCPU\tMEMORY\tUPTIME"); err != nil {
			return err
		}
		for _, row := range report.Nodes.Rows {
			if _, err := fmt.Fprintf(
				tw,
				"%s\t%s\t%.0f%%\t%s/%s\t%s\n",
				row.Name,
				empty(row.Status),
				row.CPU*100,
				FormatBytes(row.Mem),
				FormatBytes(row.MaxMem),
				FormatUptime(row.Uptime),
			); err != nil {
				return err
			}
		}

		if _, err := fmt.Fprintln(tw, "\nGUESTS"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(tw, "TOTAL\tRUNNING\tSTOPPED\tVM\tLXC"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(
			tw,
			"%d\t%d\t%d\t%d\t%d\n",
			report.Guests.Total,
			report.Guests.Running,
			report.Guests.Stopped,
			report.Guests.VM,
			report.Guests.LXC,
		); err != nil {
			return err
		}

		if _, err := fmt.Fprintln(tw, "\nSTORAGE"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(tw, "NAME\tNODE\tUSED\tSTATUS"); err != nil {
			return err
		}
		for _, row := range report.Storages.Rows {
			if _, err := fmt.Fprintf(
				tw,
				"%s\t%s\t%.0f%%\t%s\n",
				row.Storage,
				row.Node,
				row.UsedFraction*100,
				storageStatusText(row),
			); err != nil {
				return err
			}
		}

		if _, err := fmt.Fprintln(tw, "\nBACKUP"); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(tw, "COUNT\tLATEST"); err != nil {
			return err
		}
		latest := "-"
		if report.Backups.LatestCtime > 0 {
			age := uint64(time.Now().Unix()) - report.Backups.LatestCtime
			latest = FormatUptime(age) + " ago"
		}
		if _, err := fmt.Fprintf(tw, "%d\t%s\n", report.Backups.Count, latest); err != nil {
			return err
		}

		if len(report.Issues) > 0 {
			if _, err := fmt.Fprintln(tw, "\nISSUES"); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(tw, "COMPONENT\tMESSAGE"); err != nil {
				return err
			}
			for _, issue := range report.Issues {
				if _, err := fmt.Fprintf(tw, "%s\t%s\n", issue.Component, issue.Message); err != nil {
					return err
				}
			}
		}
		return tw.Flush()
	})
}

func storageStatusText(row StorageRow) string {
	if !row.Enabled {
		return "disabled"
	}
	if !row.Active {
		return "inactive"
	}
	return "active"
}

func FormatBytes(n uint64) string {
	if n == 0 {
		return "0B"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	value := float64(n)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%dB", n)
	}
	return fmt.Sprintf("%.1f%s", value, units[unit])
}

func FormatUptime(seconds uint64) string {
	if seconds == 0 {
		return "-"
	}
	d := time.Duration(seconds) * time.Second
	days := d / (24 * time.Hour)
	d -= days * 24 * time.Hour
	hours := d / time.Hour
	d -= hours * time.Hour
	mins := d / time.Minute

	parts := make([]string, 0, 3)
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if mins > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%dm", mins))
	}
	return strings.Join(parts, "")
}

func empty(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func formatBool(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func formatOptionalBytes(value uint64) string {
	if value == 0 {
		return "-"
	}
	return FormatBytes(value)
}

func formatOptionalUint(value uint64) string {
	if value == 0 {
		return "-"
	}
	return fmt.Sprint(value)
}

func formatUnixTime(value int64) string {
	if value <= 0 {
		return "-"
	}
	return time.Unix(value, 0).UTC().Format(time.RFC3339)
}
