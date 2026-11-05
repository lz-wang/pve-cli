package cmd

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/urfave/cli/v2"
)

// Only these read-only entry points accept multiple profiles.
func enableProfileQueries(commands []*cli.Command, prefix string) {
	for _, command := range commands {
		path := strings.TrimSpace(prefix + " " + command.Name)
		switch path {
		case "status", "check", "guest ls", "storage usage", "task ls", "backup audit":
			wrapProfileQuery(command, path)
		}
		enableProfileQueries(command.Subcommands, path)
	}
}

func wrapProfileQuery(command *cli.Command, path string) {
	command.Flags = append(command.Flags,
		&cli.BoolFlag{Name: "all-profiles", Usage: "query all configured profiles (read-only)"},
		&cli.StringSliceFlag{Name: "profiles", Usage: "profile names, comma-separated or repeatable"},
		&cli.IntFlag{Name: "workers", Value: 4, Usage: "parallel profile queries (1-8)"},
	)
	action := command.Action
	command.Action = func(c *cli.Context) error {
		if !c.Bool("all-profiles") && !c.IsSet("profiles") {
			return action(c)
		}
		if c.Bool("all-profiles") && c.IsSet("profiles") || c.String("profile") != "" {
			return fmt.Errorf("%w: choose --profile, --profiles or --all-profiles", pve.ErrInvalid)
		}
		if c.Int("workers") < 1 || c.Int("workers") > 8 {
			return fmt.Errorf("%w: workers must be 1..8", pve.ErrInvalid)
		}
		cfg, err := config.Load(c.String("config"))
		if err != nil {
			return err
		}
		names := []string{}
		if c.Bool("all-profiles") {
			for name := range cfg.Profiles {
				names = append(names, name)
			}
		} else {
			seen := map[string]bool{}
			for _, name := range c.StringSlice("profiles") {
				name = strings.TrimSpace(name)
				if name == "" {
					return fmt.Errorf("%w: empty profile name", pve.ErrInvalid)
				}
				if !seen[name] {
					names = append(names, name)
					seen[name] = true
				}
			}
		}
		if len(names) == 0 {
			return fmt.Errorf("%w: no profiles selected", pve.ErrInvalid)
		}
		sort.Strings(names)
		for _, name := range names {
			if _, ok := cfg.Profiles[name]; !ok {
				return fmt.Errorf("%w: unknown profile %q", pve.ErrInvalid, name)
			}
		}
		format := output.NormalizeFormat(stringFlag(c, "output"))
		if err := output.ValidateFormat(format); err != nil {
			return err
		}
		report := output.ProfileReport{Complete: true, Results: make([]output.ProfileResult, len(names))}
		jobs := make(chan int)
		var wg sync.WaitGroup
		for w := 0; w < c.Int("workers"); w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					row := output.ProfileResult{Profile: names[i], Complete: true}
					if err := c.Context.Err(); err != nil {
						row.Complete = false
						row.Error = err.Error()
						report.Results[i] = row
						continue
					}
					var stdout, stderr bytes.Buffer
					app := *c.App
					app.Writer = &stdout
					app.ErrWriter = &stderr
					fs := flag.NewFlagSet(path, flag.ContinueOnError)
					fs.String("profile", names[i], "")
					fs.String("output", "json", "")
					_ = fs.Set("profile", names[i])
					_ = fs.Set("output", "json")
					_ = fs.Parse(c.Args().Slice())
					child := cli.NewContext(&app, fs, c)
					child.Context = c.Context
					child.Command = c.Command
					err := action(child)
					if stdout.Len() > 0 {
						decoder := json.NewDecoder(&stdout)
						decoder.UseNumber()
						if e := decoder.Decode(&row.Data); e != nil {
							err = fmt.Errorf("decode profile result: %w", e)
						}
					}
					row.Data = profileNumbers(row.Data)
					if err != nil {
						row.Complete = false
						row.Error = err.Error()
					}
					if data, ok := row.Data.(map[string]any); ok {
						if complete, exists := data["complete"]; exists && complete == false {
							row.Complete = false
						}
					}
					if path == "check" {
						if rows, ok := row.Data.([]any); ok {
							for _, item := range rows {
								if check, ok := item.(map[string]any); ok && check["status"] == "unknown" {
									row.Complete = false
								}
							}
						}
					}
					report.Results[i] = row
				}
			}()
		}
		for i := range names {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		for _, row := range report.Results {
			if !row.Complete {
				report.Complete = false
			}
		}
		err = output.Write(c.App.Writer, format, report, func(w io.Writer) error {
			for _, row := range report.Results {
				if _, e := fmt.Fprintf(w, "PROFILE %s (complete=%t)\n", row.Profile, row.Complete); e != nil {
					return e
				}
				if row.Data != nil {
					if e := renderProfileData(w, path, row.Data); e != nil {
						return e
					}
				}
				if row.Error != "" {
					if _, e := fmt.Fprintln(w, "ERROR:", row.Error); e != nil {
						return e
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if !report.Complete {
			return fmt.Errorf("profile query incomplete or checks failed")
		}
		return nil
	}
}

func renderProfileData(w io.Writer, path string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	switch path {
	case "status":
		var v output.StatusReport
		if err = json.Unmarshal(b, &v); err == nil {
			return output.WriteStatusReport(w, "table", v)
		}
	case "check":
		var v []output.CheckRow
		if err = json.Unmarshal(b, &v); err == nil {
			return output.WriteCheckRows(w, "table", v)
		}
	case "guest ls":
		var v []output.GuestRow
		if err = json.Unmarshal(b, &v); err == nil {
			return output.WriteGuestRowsWithKind(w, "table", v)
		}
	case "storage usage":
		var v []output.StorageRow
		if err = json.Unmarshal(b, &v); err == nil {
			return output.WriteStorageUsageRows(w, "table", v)
		}
	case "task ls":
		var v []output.TaskRow
		if err = json.Unmarshal(b, &v); err == nil {
			return output.WriteTaskRows(w, "table", v)
		}
	default:
		_, err = fmt.Fprintln(w, string(b))
	}
	return err
}

// Keep integer values numeric in YAML as well as JSON.
func profileNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		if n, e := strconv.ParseInt(string(x), 10, 64); e == nil {
			return n
		}
		if n, e := strconv.ParseUint(string(x), 10, 64); e == nil {
			return n
		}
		n, _ := x.Float64()
		return n
	case []any:
		for i := range x {
			x[i] = profileNumbers(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = profileNumbers(x[k])
		}
	}
	return v
}
