package cmd

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/lz-wang/pve-cli/v3/internal/ssh"
)

// newLXCMigrateSubcommands holds the cross-profile LXC migration commands.
// The parent `lxc migrate VMID` keeps its intra-cluster behavior; these
// subcommands target independent PVE hosts.
func newLXCMigrateSubcommands(deps Dependencies) []*cli.Command {
	return []*cli.Command{lxcMigratePlanCommand(deps), lxcMigrateRemoteCommand(deps), lxcMigrateCheckCommand(deps)}
}

func crossProfileMigrationFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "from", Usage: "source profile name (defaults to the current profile)"},
		&cli.StringFlag{Name: "to", Usage: "target profile name", Required: true},
		&cli.StringFlag{Name: "target-node", Usage: "target PVE node name", Required: true},
		&cli.IntFlag{Name: "target-vmid", Usage: "target VMID (defaults to the source VMID)"},
		&cli.StringSliceFlag{Name: "storage-map", Usage: "repeatable source=target storage mapping"},
		&cli.StringSliceFlag{Name: "bridge-map", Usage: "repeatable source=target bridge mapping"},
	}
}

func lxcMigratePlanCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "plan",
		Usage:     "Read-only cross-profile migration preflight (no mutations)",
		ArgsUsage: "VMID",
		Flags:     append(commonOutputFlags(), crossProfileMigrationFlags()...),
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
			if options.targetNode == "" {
				return fmt.Errorf("%w: --target-node is required", pve.ErrInvalid)
			}

			sides, err := buildCrossProfileSides(c, deps, options)
			if err != nil {
				return err
			}
			plan, err := pve.NewLXCService(sides.sourceBackend, pve.TaskRunner{}, nil, false).
				PlanRemoteMigration(c.Context, vmid, sides.migrationOptions())
			if err != nil {
				return err
			}
			if err := output.WriteMigrationPlan(c.App.Writer, crossProfileOutputFormat(c, sides), plan); err != nil {
				return err
			}
			if plan.Status == "failed" {
				return fmt.Errorf("migration plan failed: resolve the failing checks before migrating")
			}
			return nil
		},
	}
}

func lxcMigrateRemoteCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "remote",
		Usage:     "Migrate a container to an independent PVE host (experimental upstream feature)",
		ArgsUsage: "VMID",
		Flags: append(append(commonOutputFlags(), crossProfileMigrationFlags()...),
			&cli.StringFlag{Name: "target-fingerprint", Usage: "SHA256 fingerprint of the target API certificate for TLS verification"},
			&cli.UintFlag{Name: "bwlimit", Usage: "migration bandwidth limit in KiB/s"},
			&cli.BoolFlag{Name: "online", Usage: "request online migration (default is offline)"},
			&cli.BoolFlag{Name: "yes", Usage: "confirm the migration and the credential transfer to the source PVE"},
			&cli.BoolFlag{Name: "wait", Usage: "wait for async task completion"},
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
			if !c.Bool("yes") {
				return fmt.Errorf("%w: remote migration requires --yes (the source PVE receives target API credentials for the transfer; the source container is kept and the target is not started)", pve.ErrInvalid)
			}
			options, err := parseCrossProfileOptions(c)
			if err != nil {
				return err
			}
			if options.targetNode == "" {
				return fmt.Errorf("%w: --target-node is required", pve.ErrInvalid)
			}

			sides, err := buildCrossProfileSides(c, deps, options)
			if err != nil {
				return err
			}
			sourceService := pve.NewLXCService(sides.sourceBackend, pve.TaskRunner{}, nil, false)

			// Preflight with the same plan command: failing checks abort
			// before any mutation.
			plan, err := sourceService.PlanRemoteMigration(c.Context, vmid, sides.migrationOptions())
			if err != nil {
				return err
			}
			if err := output.WriteMigrationPlan(c.App.ErrWriter, "table", plan); err != nil {
				return err
			}
			if plan.Status == "failed" {
				return fmt.Errorf("migration preflight failed; no changes were made")
			}

			bundle, err := buildTargetEndpointBundle(sides.targetProfile, c.String("target-fingerprint"))
			if err != nil {
				return err
			}
			fmt.Fprintln(c.App.ErrWriter, "notice: the source PVE receives target API credentials for this migration; the source container is kept, the target container is not started")
			fmt.Fprintln(c.App.ErrWriter, "notice: PVE remote migration is experimental upstream; an interrupted migration does not resume automatically")

			waitTimeout := c.Duration("wait-timeout")
			if waitTimeout <= 0 {
				waitTimeout = 30 * time.Minute
			}
			runner := pve.TaskRunner{
				Wait:         c.Bool("wait"),
				WaitTimeout:  waitTimeout,
				ErrWriter:    c.App.ErrWriter,
				ResultWriter: c.App.Writer,
				Format:       crossProfileOutputFormat(c, sides),
				Resource:     "lxc",
			}
			sourceService = pve.NewLXCService(sides.sourceBackend, runner, nil, false)
			_, err = sourceService.RemoteMigrate(c.Context, vmid, "", pve.RemoteMigrateOptions{
				TargetEndpointBundle: bundle,
				TargetStorageMap:     options.storageMap,
				TargetBridgeMap:      options.bridgeMap,
				TargetVMID:           options.targetVMID,
				BWLimit:              uint(c.Uint("bwlimit")),
				Online:               c.Bool("online"),
			})
			return err
		},
	}
}

// buildTargetEndpointBundle assembles PVE's remote-migration endpoint string
// in the official comma-separated attribute format: apitoken=...,host=...[,port=...][,fingerprint=...].
// A URL without a port defers to PVE's default (8006). The bundle embeds the
// target token secret and is only ever passed into the API call.
func buildTargetEndpointBundle(profile config.Profile, fingerprint string) (string, error) {
	secret, err := config.ResolveTokenSecret(profile)
	if err != nil {
		return "", fmt.Errorf("config error: %w", err)
	}
	parsed, err := url.Parse(strings.TrimSpace(profile.Endpoint))
	if err != nil {
		return "", fmt.Errorf("%w: invalid target endpoint %q: %v", pve.ErrInvalid, profile.Endpoint, err)
	}
	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("%w: target endpoint %q has no host", pve.ErrInvalid, profile.Endpoint)
	}
	parts := []string{
		"apitoken=PVEAPIToken=" + profile.TokenID + "=" + secret,
		"host=" + host,
	}
	if port := parsed.Port(); port != "" {
		parts = append(parts, "port="+port)
	}
	if trimmed := strings.TrimSpace(fingerprint); trimmed != "" {
		parts = append(parts, "fingerprint="+trimmed)
	}
	return strings.Join(parts, ","), nil
}

func lxcMigrateCheckCommand(deps Dependencies) *cli.Command {
	return &cli.Command{
		Name:      "check",
		Usage:     "Validate both sides after a cross-profile migration (read-only)",
		ArgsUsage: "TARGET-VMID",
		Flags: append(commonOutputFlags(),
			&cli.StringFlag{Name: "from", Usage: "source profile name (defaults to the current profile)"},
			&cli.StringFlag{Name: "to", Usage: "target profile name", Required: true},
			&cli.StringFlag{Name: "target-node", Usage: "target PVE node name"},
			&cli.IntFlag{Name: "source-vmid", Usage: "source VMID (defaults to the target VMID)"},
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
			sides, err := buildCrossProfileSides(c, deps, options)
			if err != nil {
				return err
			}
			plan, err := pve.NewLXCService(sides.sourceBackend, pve.TaskRunner{}, nil, false).
				CheckRemoteMigration(c.Context, vmid, pve.RemoteMigrationCheckOptions{
					SourceVMID: c.Int("source-vmid"),
					Target:     sides.targetBackend,
					TargetNode: options.targetNode,
				})
			if err != nil {
				return err
			}
			if err := output.WriteMigrationPlan(c.App.Writer, crossProfileOutputFormat(c, sides), plan); err != nil {
				return err
			}
			if plan.Status == "failed" {
				return fmt.Errorf("post-migration validation failed; both sides are preserved — repair manually and re-run")
			}
			return nil
		},
	}
}

func parseMigrationVMID(value string) (int, error) {
	vmid, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || vmid <= 0 {
		return 0, fmt.Errorf("%w: a numeric source VMID is required", pve.ErrInvalid)
	}
	return vmid, nil
}

type crossProfileMigrationRequest struct {
	from       string
	to         string
	targetNode string
	targetVMID int
	storageMap map[string]string
	bridgeMap  map[string]string
}

func parseCrossProfileOptions(c *cli.Context) (crossProfileMigrationRequest, error) {
	request := crossProfileMigrationRequest{
		from:       strings.TrimSpace(c.String("from")),
		to:         strings.TrimSpace(c.String("to")),
		targetNode: strings.TrimSpace(c.String("target-node")),
		targetVMID: c.Int("target-vmid"),
	}
	if request.to == "" {
		return request, fmt.Errorf("%w: --to is required", pve.ErrInvalid)
	}
	var err error
	if request.storageMap, err = parseMigrationMap(c.StringSlice("storage-map")); err != nil {
		return request, fmt.Errorf("%w: --storage-map: %v", pve.ErrInvalid, err)
	}
	if request.bridgeMap, err = parseMigrationMap(c.StringSlice("bridge-map")); err != nil {
		return request, fmt.Errorf("%w: --bridge-map: %v", pve.ErrInvalid, err)
	}
	return request, nil
}

func parseMigrationMap(entries []string) (map[string]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	result := make(map[string]string, len(entries))
	for _, entry := range entries {
		source, target, found := strings.Cut(entry, "=")
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if !found || source == "" || target == "" {
			return nil, fmt.Errorf("expected source=target, got %q", entry)
		}
		result[source] = target
	}
	return result, nil
}

// crossProfileSides carries the two independent cluster connections.
type crossProfileSides struct {
	request           crossProfileMigrationRequest
	sourceProfileName string
	sourceProfile     config.Profile
	sourceSSHProfile  *ssh.Profile
	targetProfile     config.Profile
	targetSSHProfile  *ssh.Profile
	sourceBackend     pve.Backend
	targetBackend     pve.Backend
}

func (s crossProfileSides) migrationOptions() pve.CrossProfileMigrationOptions {
	return pve.CrossProfileMigrationOptions{
		TargetNode: s.request.targetNode,
		TargetVMID: s.request.targetVMID,
		StorageMap: s.request.storageMap,
		BridgeMap:  s.request.bridgeMap,
		Target:     s.targetBackend,
	}
}

func buildCrossProfileSides(c *cli.Context, deps Dependencies, request crossProfileMigrationRequest) (*crossProfileSides, error) {
	deps = deps.withDefaults()
	cfg, err := config.Load(c.String("config"))
	if err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}
	sourceName := request.from
	if sourceName == "" {
		sourceName = c.String("profile")
	}
	sourceProfileName, sourceProfile, err := cfg.SelectProfile(sourceName)
	if err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}
	_, targetProfile, err := cfg.SelectProfile(request.to)
	if err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}
	if sourceProfileName == request.to {
		return nil, fmt.Errorf("%w: --to must be a different profile than the source", pve.ErrInvalid)
	}

	// The remote migration hands the target token to the source PVE, so the
	// source backend's sanitizer must know the target secret: PVE validation
	// errors echo the submitted endpoint bundle back.
	targetSecret, err := config.ResolveTokenSecret(targetProfile)
	if err != nil {
		return nil, fmt.Errorf("target profile %s: config error: %w", request.to, err)
	}
	sourceBackend, err := crossProfileBackend(c, deps, sourceProfile, targetSecret)
	if err != nil {
		return nil, fmt.Errorf("source profile %s: %w", sourceProfileName, err)
	}
	targetBackend, err := crossProfileBackend(c, deps, targetProfile)
	if err != nil {
		return nil, fmt.Errorf("target profile %s: %w", request.to, err)
	}
	return &crossProfileSides{
		request:           request,
		sourceProfileName: sourceProfileName,
		sourceProfile:     sourceProfile,
		sourceSSHProfile:  sourceProfile.SSH,
		targetProfile:     targetProfile,
		targetSSHProfile:  targetProfile.SSH,
		sourceBackend:     sourceBackend,
		targetBackend:     targetBackend,
	}, nil
}

// sshRunnersFor builds the SSH transports of both profiles; commands that
// need host access fail with a clear error when a profile has no ssh
// section.
func sshRunnersFor(deps Dependencies, sides *crossProfileSides) (pve.CommandRunner, pve.CommandRunner, error) {
	deps = deps.withDefaults()
	if sides.sourceSSHProfile == nil {
		return nil, nil, fmt.Errorf("%w: source profile %s has no ssh section", pve.ErrInvalid, sides.sourceProfileName)
	}
	if sides.targetSSHProfile == nil {
		return nil, nil, fmt.Errorf("%w: target profile %s has no ssh section", pve.ErrInvalid, sides.request.to)
	}
	return deps.SSHRunnerFactory(*sides.sourceSSHProfile), deps.SSHRunnerFactory(*sides.targetSSHProfile), nil
}

// crossProfileOutputFormat resolves -o with the source profile's
// default_output applied, matching the runtime commands' behavior.
func crossProfileOutputFormat(c *cli.Context, sides *crossProfileSides) string {
	format := c.String("output")
	if format == "" {
		format = sides.sourceProfile.DefaultOutput
	}
	return output.NormalizeFormat(format)
}

// requireSSHNodeBindingForTransfer refuses a host-transfer unless the
// profile's SSH host is explicitly bound (ssh.node) to the PVE node the
// data lives on — the same wrong-host hazard the pct commands close, and
// stricter: every operation that reads, modifies or transfers host data
// requires the explicit binding. A binding that disagrees with the resolved
// node is refused as well; single-host setups are safe by declaring the
// node, multi-node clusters must not guess.
func requireSSHNodeBindingForTransfer(profile *ssh.Profile, node, command string) (string, error) {
	if profile == nil {
		return "", fmt.Errorf("%w: %s transfers host data and requires an ssh profile with an explicit ssh.node binding", pve.ErrInvalid, command)
	}
	declared := strings.TrimSpace(profile.Node)
	if declared == "" {
		return "", fmt.Errorf("%w: %s transfers host data and requires an explicit ssh.node binding for the profile host %q; declare the PVE node the SSH host serves", pve.ErrInvalid, command, profile.Host)
	}
	if declared != node {
		return "", fmt.Errorf("%w: the profile ssh host is bound to PVE node %q but %s resolves to node %q; refusing to run host commands there", pve.ErrInvalid, declared, command, node)
	}
	return declared, nil
}

// resolveSourceNodeForArchive finds the PVE node whose storage lists the
// archive volume; the SSH host name never decides the node.
func resolveSourceNodeForArchive(ctx context.Context, backend pve.Backend, volid string) (string, error) {
	storage, _, _ := strings.Cut(volid, ":")
	if storage == "" {
		return "", fmt.Errorf("%w: invalid volume id %q", pve.ErrInvalid, volid)
	}
	nodes, err := backend.Nodes(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve source node: %w", err)
	}
	var firstErr error
	for _, node := range nodes {
		if strings.TrimSpace(node.Name) == "" {
			continue
		}
		rows, err := backend.Backups(ctx, node.Name, storage)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, row := range rows {
			if row.VolID == volid {
				return node.Name, nil
			}
		}
	}
	if firstErr != nil {
		return "", fmt.Errorf("resolve source node for %s: %w", volid, firstErr)
	}
	return "", fmt.Errorf("%w: archive %s not found on any source node; pass --node explicitly", pve.ErrNotFound, volid)
}

func crossProfileBackend(c *cli.Context, deps Dependencies, profile config.Profile, extraSecrets ...string) (pve.Backend, error) {
	secret, err := config.ResolveTokenSecret(profile)
	if err != nil {
		return nil, err
	}
	timeout, err := resolveAPIRequestTimeout(c, profile)
	if err != nil {
		return nil, err
	}
	return deps.BackendFactory(profile, pve.ClientOptions{
		TokenSecret:  secret,
		ExtraSecrets: extraSecrets,
		Timeout:      timeout,
		Insecure:     boolFlag(c, "insecure"),
	})
}
