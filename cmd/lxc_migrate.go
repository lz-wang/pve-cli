package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
)

// newLXCMigrateSubcommands holds the cross-profile LXC migration commands.
// The parent `lxc migrate VMID` keeps its intra-cluster behavior; these
// subcommands target independent PVE hosts.
func newLXCMigrateSubcommands(deps Dependencies) []*cli.Command {
	return []*cli.Command{lxcMigratePlanCommand(deps)}
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

			sides, err := buildCrossProfileSides(c, deps, options)
			if err != nil {
				return err
			}
			plan, err := pve.NewLXCService(sides.sourceBackend, pve.TaskRunner{}, nil, false).
				PlanRemoteMigration(c.Context, vmid, sides.migrationOptions())
			if err != nil {
				return err
			}
			if err := output.WriteMigrationPlan(c.App.Writer, output.NormalizeFormat(c.String("output")), plan); err != nil {
				return err
			}
			if plan.Status == "failed" {
				return fmt.Errorf("migration plan failed: resolve the failing checks before migrating")
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
	if request.targetNode == "" {
		return request, fmt.Errorf("%w: --target-node is required", pve.ErrInvalid)
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
	request       crossProfileMigrationRequest
	sourceProfile config.Profile
	sourceBackend pve.Backend
	targetBackend pve.Backend
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

	sourceBackend, err := crossProfileBackend(c, deps, sourceProfile)
	if err != nil {
		return nil, fmt.Errorf("source profile %s: %w", sourceProfileName, err)
	}
	targetBackend, err := crossProfileBackend(c, deps, targetProfile)
	if err != nil {
		return nil, fmt.Errorf("target profile %s: %w", request.to, err)
	}
	return &crossProfileSides{
		request:       request,
		sourceProfile: sourceProfile,
		sourceBackend: sourceBackend,
		targetBackend: targetBackend,
	}, nil
}

func crossProfileBackend(c *cli.Context, deps Dependencies, profile config.Profile) (pve.Backend, error) {
	secret, err := config.ResolveTokenSecret(profile)
	if err != nil {
		return nil, err
	}
	timeout, err := resolveAPIRequestTimeout(c, profile)
	if err != nil {
		return nil, err
	}
	return deps.BackendFactory(profile, pve.ClientOptions{
		TokenSecret: secret,
		Timeout:     timeout,
		Insecure:    boolFlag(c, "insecure"),
	})
}
