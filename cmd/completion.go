package cmd

import (
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/urfave/cli/v2"
	"io"
	"sort"
	"strings"
)

func newCompletionCommand() *cli.Command {
	command := &cli.Command{Name: "completion", Usage: "Print shell completion scripts"}
	scripts := map[string]string{
		"bash": `_pve_complete() { local IFS=$'\n'; COMPREPLY=($(pve _complete "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null)); }
complete -F _pve_complete pve
`,
		"zsh": `#compdef pve
_pve_complete() { local -a candidates; candidates=("${(@f)$(pve _complete "${words[@]:1}" 2>/dev/null)}"); compadd -- "${candidates[@]}"; }
compdef _pve_complete pve
`,
		"fish": `function __pve_complete
 set -l words (commandline -opc)
 set -e words[1]
 command pve _complete $words (commandline -ct) 2>/dev/null
end
complete -c pve -f -a '(__pve_complete)'
`,
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script := scripts[shell]
		command.Subcommands = append(command.Subcommands, &cli.Command{Name: shell, Usage: "Print " + shell + " completions", Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 0); err != nil {
				return err
			}
			_, err := io.WriteString(c.App.Writer, script)
			return err
		}})
	}
	return command
}
func newCompleteCommand(deps Dependencies) *cli.Command {
	return &cli.Command{Name: "_complete", Hidden: true, SkipFlagParsing: true, Action: func(c *cli.Context) error {
		words := c.Args().Slice()
		prefix := ""
		if len(words) > 0 {
			prefix = words[len(words)-1]
			words = words[:len(words)-1]
		}
		commands := c.App.Commands
		var command *cli.Command
		previous := ""
		node := ""
		for i := 0; i < len(words); i++ {
			word := words[i]
			if strings.HasPrefix(word, "-") {
				previous = word
				if i+1 < len(words) && !strings.HasPrefix(words[i+1], "-") {
					switch word {
					case "--node":
						node = words[i+1]
						i++
					case "--config", "--profile":
						if err := c.Set(strings.TrimPrefix(word, "--"), words[i+1]); err != nil {
							return nil
						}
						i++
					}
				}
				continue
			}
			for _, candidate := range commands {
				if candidate.Name == word {
					command = candidate
					commands = candidate.Subcommands
					break
				}
			}
			if i == len(words)-1 {
				previous = word
			}
		}
		candidates := []string{}
		if len(words) > 0 {
			previous = words[len(words)-1]
		}
		enums := map[string][]string{"--output": {"table", "json", "yaml"}, "-o": {"table", "json", "yaml"}, "--type": {"vm", "lxc", "all"}, "--status": {"running", "stopped"}, "--sort": {"vmid", "name", "node", "status", "memory", "cpu"}}
		if values, ok := enums[previous]; ok {
			candidates = append(candidates, values...)
		} else {
			for _, item := range commands {
				if !item.Hidden {
					candidates = append(candidates, item.Name)
				}
			}
			flags := c.App.Flags
			if command != nil {
				flags = append(append([]cli.Flag{}, flags...), command.Flags...)
			}
			for _, flag := range flags {
				for _, name := range flag.Names() {
					if len(name) > 1 {
						candidates = append(candidates, "--"+name)
					}
				}
			}
			// Dynamic completion only calls read capabilities. All failures are silent.
			if previous == "--node" || previous == "--storage" || (command != nil && strings.Contains(command.ArgsUsage, "VMID")) {
				rt, err := buildRuntime(c, Dependencies{BackendFactory: deps.BackendFactory, Stdin: deps.Stdin, Stdout: io.Discard, Stderr: io.Discard})
				if err == nil {
					switch previous {
					case "--node":
						rows, e := rt.backend.Nodes(c.Context)
						if e == nil {
							for _, r := range rows {
								candidates = append(candidates, r.Name)
							}
						}
					case "--storage":
						rows, e := pve.NewStorageService(rt.backend).List(c.Context, pve.StorageListOptions{Node: node})
						if e == nil {
							for _, r := range rows {
								candidates = append(candidates, r.Storage)
							}
						}
					default:
						rows, e := pve.NewGuestAggregateService(rt.backend, nil, false).List(c.Context, pve.GuestListOptions{Node: node})
						if e == nil {
							for _, r := range rows {
								candidates = append(candidates, fmt.Sprint(r.VMID))
							}
						}
					}
				}
			}
		}
		sort.Strings(candidates)
		last := ""
		for _, candidate := range candidates {
			if candidate == last || !strings.HasPrefix(candidate, prefix) {
				continue
			}
			last = candidate
			if _, err := fmt.Fprintln(c.App.Writer, candidate); err != nil {
				return err
			}
		}
		return nil
	}}
}
