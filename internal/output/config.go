package output

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// ConfigProfileRow contains display values for a local configuration profile.
// TokenSecret must contain a summary, never the complete stored secret.
type ConfigProfileRow struct {
	Name               string
	Current            bool
	Endpoint           string
	TokenID            string
	TokenSecret        string
	TokenSecretEnv     string
	InsecureSkipVerify bool
	Timeout            string
	DefaultOutput      string
}

// WriteConfigProfileList renders only profile names and endpoints.
func WriteConfigProfileList(w io.Writer, rows []ConfigProfileRow) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "PROFILE\tENDPOINT"); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(tw, "%s\t%s\n", configCell(row.Name), configCell(row.Endpoint)); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// WriteConfigProfiles renders each profile as a field/value table.
func WriteConfigProfiles(w io.Writer, rows []ConfigProfileRow) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if len(rows) == 0 {
		if _, err := fmt.Fprintln(tw, "FIELD\tVALUE"); err != nil {
			return err
		}
	}
	for i, row := range rows {
		if i > 0 {
			if _, err := fmt.Fprintln(tw); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(tw, "FIELD\tVALUE"); err != nil {
			return err
		}
		fields := [][2]string{
			{"Profile", row.Name},
			{"Current", formatBool(row.Current)},
			{"Endpoint", row.Endpoint},
			{"Token ID", row.TokenID},
			{"Token secret", row.TokenSecret},
			{"Token secret env", row.TokenSecretEnv},
			{"Skip TLS verify", formatBool(row.InsecureSkipVerify)},
			{"Timeout", row.Timeout},
			{"Default output", row.DefaultOutput},
		}
		for _, field := range fields {
			if _, err := fmt.Fprintf(tw, "%s\t%s\n", field[0], configCell(field[1])); err != nil {
				return err
			}
		}
	}
	return tw.Flush()
}

func configCell(value string) string {
	return strings.NewReplacer(
		"\t", "\\t", "\r", "\\r", "\n", "\\n",
		"\u0085", "\\u0085", "\u2028", "\\u2028", "\u2029", "\\u2029",
	).Replace(empty(value))
}
