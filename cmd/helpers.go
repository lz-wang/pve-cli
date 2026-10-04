package cmd

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/output"
)

func parseVMID(value string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("vmid is required")
	}
	vmid, err := strconv.Atoi(value)
	if err != nil || vmid <= 0 {
		return 0, fmt.Errorf("invalid vmid %q", value)
	}
	return vmid, nil
}

func requireNoExtraArgs(c *cli.Context, want int) error {
	if c.NArg() == want {
		return nil
	}
	return fmt.Errorf("expected %d argument(s), got %d", want, c.NArg())
}

func parseSetFlags(items []string) (map[string]string, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("at least one --set key=value is required")
	}

	result := make(map[string]string, len(items))
	for _, item := range items {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid --set %q, expected key=value", item)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("empty key in --set %q", item)
		}
		result[key] = value
	}
	return result, nil
}

func parseSnapshotName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", fmt.Errorf("snapshot name is required")
	}
	return name, nil
}

func confirmDelete(in io.Reader, out io.Writer, kind string, vmid uint64, node string) error {
	if in == nil {
		return fmt.Errorf("delete aborted")
	}
	if out != nil {
		fmt.Fprintf(out, "delete %s %d on node %s? type %d to confirm: ", kind, vmid, node, vmid)
	}
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return fmt.Errorf("delete aborted")
	}
	if strings.TrimSpace(scanner.Text()) != strconv.FormatUint(vmid, 10) {
		return fmt.Errorf("delete aborted")
	}
	return nil
}

func confirmRollback(in io.Reader, out io.Writer, kind string, vmid uint64, node, snapshot string) error {
	if in == nil {
		return fmt.Errorf("rollback aborted")
	}
	if out != nil {
		fmt.Fprintf(out, "rollback %s %d on node %s to snapshot %s? type %q to confirm: ", kind, vmid, node, snapshot, snapshot)
	}
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return fmt.Errorf("rollback aborted")
	}
	if strings.TrimSpace(scanner.Text()) != snapshot {
		return fmt.Errorf("rollback aborted")
	}
	return nil
}

func confirmSnapshotDelete(in io.Reader, out io.Writer, kind string, vmid uint64, node, snapshot string) error {
	if in == nil {
		return fmt.Errorf("snapshot delete aborted")
	}
	if out != nil {
		fmt.Fprintf(out, "delete snapshot %s of %s %d on node %s? type %q to confirm: ", snapshot, kind, vmid, node, snapshot)
	}
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return fmt.Errorf("snapshot delete aborted")
	}
	if strings.TrimSpace(scanner.Text()) != snapshot {
		return fmt.Errorf("snapshot delete aborted")
	}
	return nil
}

func confirmBulkAction(in io.Reader, out io.Writer, action string, rows []output.GuestRow) error {
	if in == nil {
		return fmt.Errorf("bulk %s aborted", action)
	}
	if out != nil {
		fmt.Fprintf(out, "About to %s %d guests:\n", action, len(rows))
		for _, row := range rows {
			name := row.Name
			if name == "" {
				name = "-"
			}
			fmt.Fprintf(out, "  %s %d %s\n", row.Kind, row.VMID, name)
		}
		fmt.Fprintln(out, `Type "yes" to continue: `)
	}
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return fmt.Errorf("bulk %s aborted", action)
	}
	if strings.TrimSpace(scanner.Text()) != "yes" {
		return fmt.Errorf("bulk %s aborted", action)
	}
	return nil
}
