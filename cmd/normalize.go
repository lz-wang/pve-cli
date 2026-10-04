package cmd

import "strings"

var commandRoots = map[string]bool{
	"config":  true,
	"status":  true,
	"check":   true,
	"node":    true,
	"guest":   true,
	"task":    true,
	"backup":  true,
	"storage": true,
	"vm":      true,
	"lxc":     true,
}

var flagsWithValues = map[string]bool{
	"--config":           true,
	"--profile":          true,
	"--output":           true,
	"-o":                 true,
	"--timeout":          true,
	"--wait-timeout":     true,
	"--node":             true,
	"--content":          true,
	"--type":             true,
	"--kind":             true,
	"--vmid":             true,
	"--newid":            true,
	"--name":             true,
	"--hostname":         true,
	"--target":           true,
	"--storage":          true,
	"--mode":             true,
	"--compress":         true,
	"--notes-template":   true,
	"--bwlimit":          true,
	"--protected":        true,
	"--pool":             true,
	"--snapname":         true,
	"--description":      true,
	"--format":           true,
	"--set":              true,
	"--disk":             true,
	"--size":             true,
	"--tail":             true,
	"--limit":            true,
	"--tag":              true,
	"--tag-match":        true,
	"--endpoint":         true,
	"--token-id":         true,
	"--token-secret-env": true,
	"--default-output":   true,
}

func normalizeArgs(args []string) []string {
	if len(args) < 3 {
		return args
	}

	resourceIndex := findResourceIndex(args)
	if resourceIndex == -1 || resourceIndex+1 >= len(args) {
		return args
	}
	leafIndex := resourceIndex + 1
	if leafIndex < len(args) && (args[leafIndex] == "snapshot" || args[leafIndex] == "content" || args[leafIndex] == "agent") && leafIndex+1 < len(args) {
		leafIndex++
	}
	if strings.HasPrefix(args[leafIndex], "-") {
		return args
	}

	prefix := append([]string{}, args[:leafIndex+1]...)
	rest := args[leafIndex+1:]
	if len(rest) == 0 {
		return args
	}

	flags := make([]string, 0, len(rest))
	positionals := make([]string, 0, len(rest))
	for i := 0; i < len(rest); i++ {
		token := rest[i]
		if !strings.HasPrefix(token, "-") || token == "-" {
			positionals = append(positionals, token)
			continue
		}

		flags = append(flags, token)
		name := token
		if idx := strings.IndexRune(token, '='); idx >= 0 {
			name = token[:idx]
		}
		if flagsWithValues[name] && !strings.Contains(token, "=") && i+1 < len(rest) {
			i++
			flags = append(flags, rest[i])
		}
	}

	normalized := make([]string, 0, len(args))
	normalized = append(normalized, prefix...)
	normalized = append(normalized, flags...)
	normalized = append(normalized, positionals...)
	return normalized
}

func findResourceIndex(args []string) int {
	for i := 1; i < len(args); i++ {
		token := args[i]
		if token == "--" {
			return -1
		}
		if commandRoots[token] {
			return i
		}
		if !strings.HasPrefix(token, "-") {
			continue
		}
		name := token
		if idx := strings.IndexRune(token, '='); idx >= 0 {
			name = token[:idx]
		}
		if flagsWithValues[name] && !strings.Contains(token, "=") {
			i++
		}
	}
	return -1
}
