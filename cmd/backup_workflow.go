package cmd

import (
	"fmt"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
)

// backupCopyRestoreCommand composes backup → transfer → verify → restore →
// post-check between two independent PVE hosts. Each stage keeps its own
// result; failures never roll back.
func backupCopyRestoreCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "copy-restore",
		Usage:     "Backup, copy, verify, restore and post-check a guest on another PVE host",
		ArgsUsage: "VMID",
		Flags: append(commonOutputFlags(),
			&cli.StringFlag{Name: "from", Usage: "source profile name", Required: true},
			&cli.StringFlag{Name: "to", Usage: "target profile name", Required: true},
			&cli.StringFlag{Name: "backup-storage", Usage: "source dir storage for the backup stage"},
			&cli.StringFlag{Name: "archive", Usage: "existing source archive volid; skips the backup stage (resume)"},
			&cli.StringFlag{Name: "target-storage", Usage: "target dir storage name", Required: true},
			&cli.StringFlag{Name: "target-node", Usage: "target PVE node name", Required: true},
			&cli.IntFlag{Name: "target-vmid", Usage: "target VMID (defaults to the source VMID)"},
			&cli.StringFlag{Name: "node", Usage: "source PVE node name (defaults to the source guest's API-resolved node)"},
			&cli.StringSliceFlag{Name: "bridge-map", Usage: "repeatable source=target bridge rewrite applied after the restore"},
			&cli.BoolFlag{Name: "wait", Usage: "wait for async task completion (required for the pipeline)", Required: true},
			&cli.DurationFlag{Name: "wait-timeout", Usage: "task wait timeout"},
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			vmid, err := parseMigrationVMID(c.Args().First())
			if err != nil {
				return err
			}
			options, err := parseCrossProfileOptions(c)
			if err != nil {
				return err
			}
			bridgeMap, err := parseMigrationMap(c.StringSlice("bridge-map"))
			if err != nil {
				return fmt.Errorf("%w: --bridge-map: %v", pve.ErrInvalid, err)
			}

			sides, err := buildCrossProfileSides(c, deps, options)
			if err != nil {
				return err
			}
			sourceRunner, targetRunner, err := sshRunnersFor(deps, sides)
			if err != nil {
				return err
			}
			sourceNode := c.String("node")
			if sourceNode == "" {
				// The SSH host name is not necessarily the PVE node name;
				// the backup stage runs on the source guest's actual node.
				row, rowErr := pve.NewLXCService(sides.sourceBackend, pve.TaskRunner{}, nil, false).Get(c.Context, vmid, "")
				if rowErr != nil {
					return rowErr
				}
				sourceNode = row.Node
			}
			if bound, bindErr := requireSSHNodeBindingForTransfer(sides.sourceSSHProfile, sourceNode, "backup copy-restore"); bindErr != nil {
				return bindErr
			} else if bound == "" {
				fmt.Fprintf(c.App.ErrWriter, "notice: source ssh host is not bound to a node (ssh.node); the source guest lives on node %s\n", sourceNode)
			}
			if bound, bindErr := requireSSHNodeBindingForTransfer(sides.targetSSHProfile, c.String("target-node"), "backup copy-restore"); bindErr != nil {
				return bindErr
			} else if bound == "" {
				fmt.Fprintf(c.App.ErrWriter, "notice: target ssh host is not bound to a node (ssh.node); the target node is %s\n", c.String("target-node"))
			}

			if _, ok := sides.sourceBackend.(pve.DirStoragePath); !ok {
				return fmt.Errorf("%w: source backend cannot resolve dir storage paths", pve.ErrUnavailable)
			}
			targetDir, ok := sides.targetBackend.(pve.DirStoragePath)
			if !ok {
				return fmt.Errorf("%w: target backend cannot resolve dir storage paths", pve.ErrUnavailable)
			}
			if _, ok := sides.sourceBackend.(pve.BackupReader); !ok {
				return fmt.Errorf("%w: source backend cannot list backups", pve.ErrUnavailable)
			}

			waitTimeout := c.Duration("wait-timeout")
			if waitTimeout <= 0 {
				waitTimeout = 30 * time.Minute
			}
			backupTasks := pve.TaskRunner{Wait: true, WaitTimeout: waitTimeout, ErrWriter: c.App.ErrWriter}
			restoreTasks := pve.TaskRunner{Wait: true, WaitTimeout: waitTimeout, ErrWriter: c.App.ErrWriter}

			transfer := pve.NewBackupTransferService(
				sides.sourceBackend, sourceNode, sourceRunner, targetDir, targetRunner, c.App.ErrWriter,
			)
			workflow := pve.NewBackupCopyRestoreWorkflow(
				pve.NewBackupService(sides.sourceBackend, backupTasks),
				transfer,
				pve.NewRestoreService(sides.targetBackend, restoreTasks),
				pve.NewLXCService(sides.sourceBackend, pve.TaskRunner{}, nil, false),
				sides.targetBackend,
			)

			result, err := workflow.Run(c.Context, pve.BackupCopyRestoreOptions{
				VMID:          vmid,
				BackupStorage: c.String("backup-storage"),
				Archive:       c.String("archive"),
				TargetStorage: c.String("target-storage"),
				TargetNode:    c.String("target-node"),
				TargetVMID:    c.Int("target-vmid"),
				BridgeMap:     bridgeMap,
			})
			if writeErr := output.WriteWorkflowResult(c.App.Writer, crossProfileOutputFormat(c, sides), result); writeErr != nil {
				return writeErr
			}
			return err
		},
	}
}
