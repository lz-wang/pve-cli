package cmd

import (
	"fmt"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
)

// backupCopyCommand streams a backup archive from one PVE host to another
// over the two profiles' SSH transports. Only dir-type storages are
// supported; the target never overwrites an existing archive.
func backupCopyCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "copy",
		Usage:     "Copy a backup archive to another PVE host (dir storages, SSH transport)",
		ArgsUsage: "VOLID",
		Flags: append(commonOutputFlags(),
			&cli.StringFlag{Name: "from", Usage: "source profile name", Required: true},
			&cli.StringFlag{Name: "to", Usage: "target profile name", Required: true},
			&cli.StringFlag{Name: "target-storage", Usage: "target dir storage name", Required: true},
			&cli.StringFlag{Name: "node", Usage: "source PVE node name (defaults to API resolution from the archive's storage listing)"},
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			volid := c.Args().First()
			options, err := parseCrossProfileOptions(c)
			if err != nil {
				return err
			}

			sides, err := buildCrossProfileSides(c, deps, options)
			if err != nil {
				return err
			}
			sourceRunner, targetRunner, err := sshRunnersFor(deps, sides)
			if err != nil {
				return err
			}
			sourceReader, ok := sides.sourceBackend.(pve.BackupReader)
			if !ok {
				return fmt.Errorf("%w: source backend cannot list backups", pve.ErrUnavailable)
			}
			sourceNode := c.String("node")
			if sourceNode == "" {
				// The SSH host name is not necessarily the PVE node name;
				// resolve the node whose storage actually lists the archive.
				sourceNode, err = resolveSourceNodeForArchive(c.Context, sides.sourceBackend, volid)
				if err != nil {
					return err
				}
			}
			if bound, bindErr := requireSSHNodeBindingForTransfer(sides.sourceSSHProfile, sourceNode, "backup copy"); bindErr != nil {
				return bindErr
			} else if bound == "" {
				fmt.Fprintf(c.App.ErrWriter, "notice: source ssh host is not bound to a node (ssh.node); the archive resolves to node %s\n", sourceNode)
			}
			targetDir, ok := sides.targetBackend.(pve.DirStoragePath)
			if !ok {
				return fmt.Errorf("%w: target backend cannot resolve dir storage paths", pve.ErrUnavailable)
			}

			service := pve.NewBackupTransferService(
				sourceReader, sourceNode, sourceRunner,
				targetDir, targetRunner, c.App.ErrWriter,
			)
			result, err := service.Copy(c.Context, pve.BackupCopyOptions{
				Volid:         volid,
				TargetStorage: c.String("target-storage"),
			})
			if writeErr := output.WriteBackupCopyResult(c.App.Writer, crossProfileOutputFormat(c, sides), result); writeErr != nil {
				return writeErr
			}
			return err
		},
	}
}
