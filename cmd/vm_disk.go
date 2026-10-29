package cmd

import (
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/urfave/cli/v2"
)

func newVMDiskCommand(deps Dependencies) *cli.Command {
	list := &cli.Command{Name: "ls", Usage: "List attached and unused disks", ArgsUsage: "VMID", Flags: append(commonNodeFlag(), commonOutputFlags()...), Action: func(c *cli.Context) error {
		if e := requireNoExtraArgs(c, 1); e != nil {
			return e
		}
		id, e := resolveGuestID(c, "vm", deps, c.Args().First())
		if e != nil {
			return e
		}
		rt, e := buildRuntime(c, deps)
		if e != nil {
			return e
		}
		rows, e := pve.NewDiskService(rt.backend, rt.tasks).List(c.Context, id, c.String("node"))
		if e != nil {
			return e
		}
		return output.WriteDisks(rt.stdout, rt.format, rows)
	}}
	add := &cli.Command{Name: "add", Usage: "Allocate and attach a new disk", ArgsUsage: "VMID", Flags: append(commonControlFlags(), &cli.StringFlag{Name: "slot", Required: true}, &cli.StringFlag{Name: "storage", Required: true}, &cli.StringFlag{Name: "size", Required: true}), Action: func(c *cli.Context) error {
		if e := requireNoExtraArgs(c, 1); e != nil {
			return e
		}
		id, e := resolveGuestID(c, "vm", deps, c.Args().First())
		if e != nil {
			return e
		}
		rt, e := buildRuntime(c, deps)
		if e != nil {
			return e
		}
		result, e := pve.NewDiskService(rt.backend, rt.tasks).Add(c.Context, id, c.String("node"), c.String("slot"), c.String("storage"), c.String("size"))
		if result.Task != "" || e == nil {
			if err := output.WriteDiskResult(rt.stdout, rt.format, result); err != nil {
				return err
			}
		}
		return e
	}}
	command := &cli.Command{Name: "disk", Usage: "Manage VM disks; remove detaches, delete destroys unused volumes", Subcommands: []*cli.Command{list, add}}
	for _, action := range []string{"remove", "delete"} {
		deleteVolume := action == "delete"
		command.Subcommands = append(command.Subcommands, &cli.Command{Name: action, Usage: map[bool]string{false: "Detach a disk while retaining its volume", true: "Destroy an explicitly unused disk volume"}[deleteVolume], ArgsUsage: "VMID SLOT", Flags: append(append(commonNodeFlag(), commonOutputFlags()...), &cli.BoolFlag{Name: "yes", Usage: "skip local confirmation"}), Action: func(c *cli.Context) error {
			if e := requireNoExtraArgs(c, 2); e != nil {
				return e
			}
			id, e := resolveGuestID(c, "vm", deps, c.Args().First())
			if e != nil {
				return e
			}
			slot := c.Args().Get(1)
			rt, e := buildRuntime(c, deps)
			if e != nil {
				return e
			}
			svc := pve.NewDiskService(rt.backend, rt.tasks)
			// Read target before confirmation so the prompt identifies the actual volume.
			rows, e := svc.List(c.Context, id, c.String("node"))
			if e != nil {
				return e
			}
			var target *output.DiskRow
			for i := range rows {
				if rows[i].Slot == slot {
					target = &rows[i]
				}
			}
			if target == nil {
				return fmt.Errorf("%w: disk slot %s", pve.ErrNotFound, slot)
			}
			if !c.Bool("yes") {
				description := fmt.Sprintf("%s VM %d slot %s volume %s on %s", action, id, slot, target.Volume, target.Node)
				if e = confirmExact(deps.withDefaults().Stdin, rt.stderr, description, fmt.Sprintf("%d %s", id, slot)); e != nil {
					return e
				}
			}
			result, e := svc.Unlink(c.Context, id, target.Node, slot, target.Volume, deleteVolume)
			if result.Volume != "" {
				if err := output.WriteDiskResult(rt.stdout, rt.format, result); err != nil {
					return err
				}
			}
			return e
		}})
	}
	return command
}
