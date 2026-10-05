package main

import (
	"fmt"
	"os"

	"github.com/lz-wang/pve-cli/v2/cmd"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	if err := cmd.RunWithBuildInfo(os.Args, cmd.BuildInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
