package cmd

import (
	"fmt"
	"github.com/urfave/cli/v2"
	"strconv"
)

func guestQueryFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "name", Usage: "case-insensitive name substring"},
		&cli.StringFlag{Name: "status", Usage: "guest status"},
		&cli.StringFlag{Name: "sort", Value: "vmid", Usage: "vmid,name,node,status,memory,cpu"},
		&cli.BoolFlag{Name: "desc", Usage: "sort descending; missing metrics remain last"},
	}
}
func resolveGuestID(c *cli.Context, kind string, deps Dependencies, value string) (int, error) {
	explicitName := c.String("name")
	if c.Command.Name == "clone" {
		explicitName = ""
	}
	if explicitName == "" {
		if _, err := strconv.Atoi(value); err == nil {
			return parseVMID(value)
		}
	}
	if value == "" && explicitName == "" {
		return 0, fmt.Errorf("vmid or name is required")
	}
	if explicitName != "" && value != "" {
		return 0, fmt.Errorf("choose ID or --name")
	}
	rt, err := buildRuntime(c, deps)
	if err != nil {
		return 0, err
	}
	byName := explicitName != ""
	if byName {
		value = explicitName
	}
	ref, err := guestService(kind, rt).Resolve(c.Context, value, c.String("node"), byName)
	if err != nil {
		return 0, err
	}
	if err = c.Set("node", ref.Node); err != nil {
		return 0, err
	}
	return ref.VMID, nil
}
