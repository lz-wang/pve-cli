package cmd

import (
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/urfave/cli/v2"
)

func backupDeleteCommand(deps Dependencies) *cli.Command {
	return &cli.Command{Name: "delete", Usage: "Delete one exact unprotected vzdump archive", ArgsUsage: "ARCHIVE", Flags: append([]cli.Flag{
		&cli.StringFlag{Name: "node", Required: true}, &cli.BoolFlag{Name: "yes", Usage: "skip archive confirmation"}, &cli.BoolFlag{Name: "wait", Usage: "wait for asynchronous deletion and verify absence"}, &cli.DurationFlag{Name: "wait-timeout", Usage: "task wait timeout"}, &cli.BoolFlag{Name: "dry-run", Usage: "inspect without deleting or prompting"},
	}, commonOutputFlags()...), Action: func(c *cli.Context) error {
		if err := requireNoExtraArgs(c, 1); err != nil {
			return err
		}
		rt, err := buildRuntime(c, deps)
		if err != nil {
			return err
		}
		backend, ok := rt.backend.(pve.BackupDeleteBackend)
		if !ok {
			return fmt.Errorf("backup deletion unavailable")
		}
		archive, err := pve.InspectBackupDelete(c.Context, backend, c.String("node"), c.Args().First())
		if err != nil {
			return err
		}
		if c.Bool("dry-run") {
			return output.WriteBackupDelete(rt.stdout, rt.format, output.BackupDeleteResult{OperationResult: output.OperationResult{Resource: "backup", Node: c.String("node"), Status: "planned"}, Archive: archive.VolID})
		}
		if !boolFlag(c, "yes") {
			if err = confirmExact(deps.withDefaults().Stdin, rt.stderr, "permanently delete backup on node "+c.String("node"), archive.VolID); err != nil {
				return err
			}
		}
		result, runErr := pve.DeleteBackupArchive(c.Context, backend, rt.tasks, c.String("node"), archive)
		if err = output.WriteBackupDelete(rt.stdout, rt.format, result); err != nil {
			return err
		}
		return runErr
	}}
}
