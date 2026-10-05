package cmd

import (
	"strings"

	"github.com/urfave/cli/v2"
)

// normalizeArgs lets users write resource IDs before flags, for example
// `pve vm start 100 --wait`. It resolves the command path by walking the real
// command tree, then reorders the remaining tokens so every flag precedes the
// positional arguments, which is what urfave/cli expects. The command tree is
// the single source of truth: which flags consume a value is derived from the
// cli.Flag definitions along the resolved path instead of a hand-maintained
// list.
func normalizeArgs(app *cli.App, args []string) []string {
	if len(args) < 3 {
		return args
	}

	globalValues := valueFlagTokens(app.Flags)

	// Skip global flags to find the root command token.
	i := 1
	for ; i < len(args); i++ {
		token := args[i]
		if token == "--" {
			return args
		}
		if !strings.HasPrefix(token, "-") || token == "-" {
			break
		}
		if strings.Contains(token, "=") {
			continue
		}
		if globalValues[token] && i+1 < len(args) {
			i++ // skip the flag's value
		}
	}
	if i >= len(args) {
		return args
	}
	root := findCommand(app.Commands, args[i])
	if root == nil {
		return args
	}
	path := []*cli.Command{root}
	pathEnd := i
	i++

	// Extend the path while consecutive tokens keep naming nested commands.
	for i < len(args) {
		token := args[i]
		if token != "-" && strings.HasPrefix(token, "-") {
			break
		}
		sub := findCommand(path[len(path)-1].Subcommands, token)
		if sub == nil {
			break
		}
		path = append(path, sub)
		pathEnd = i
		i++
	}
	if pathEnd+1 >= len(args) {
		return args
	}

	// Reorder the tail: flags (with their values) first, then positionals.
	values := globalValues
	for _, command := range path {
		for token, takesValue := range valueFlagTokens(command.Flags) {
			values[token] = takesValue
		}
	}
	rest := args[pathEnd+1:]
	flags := make([]string, 0, len(rest))
	positionals := make([]string, 0, len(rest))
	for j := 0; j < len(rest); j++ {
		token := rest[j]
		// Everything after "--" belongs to the command verbatim (for example
		// `vm agent exec 100 -- /usr/bin/uname -a`); stop reordering there.
		if token == "--" {
			positionals = append(positionals, rest[j:]...)
			break
		}
		if !strings.HasPrefix(token, "-") || token == "-" {
			positionals = append(positionals, token)
			continue
		}
		flags = append(flags, token)
		if strings.Contains(token, "=") {
			continue
		}
		if values[token] && j+1 < len(rest) {
			j++
			flags = append(flags, rest[j])
		}
	}

	normalized := make([]string, 0, len(args))
	normalized = append(normalized, args[:pathEnd+1]...)
	normalized = append(normalized, flags...)
	normalized = append(normalized, positionals...)
	return normalized
}

// findCommand returns the command with the given name or alias.
func findCommand(commands []*cli.Command, name string) *cli.Command {
	for _, command := range commands {
		for _, candidate := range command.Names() {
			if candidate == name {
				return command
			}
		}
	}
	return nil
}

// valueFlagTokens maps flag tokens (--name, or -n for one-letter names) to
// whether the flag consumes the following token. Bool flags do not take a
// value; unknown flag kinds are treated as value-taking so new flag types
// reorder conservatively instead of silently eating a positional.
func valueFlagTokens(flags []cli.Flag) map[string]bool {
	values := make(map[string]bool, len(flags))
	for _, flag := range flags {
		takesValue := flagTakesValue(flag)
		for _, name := range flag.Names() {
			token := "--" + name
			if len(name) == 1 {
				token = "-" + name
			}
			values[token] = takesValue
		}
	}
	return values
}

func flagTakesValue(flag cli.Flag) bool {
	_, isBool := flag.(*cli.BoolFlag)
	return !isBool
}
