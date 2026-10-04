package cmd

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	goruntime "runtime"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pvectl/internal/config"
	"github.com/lz-wang/pvectl/internal/output"
	"github.com/lz-wang/pvectl/internal/pve"
)

type BackendFactory func(config.Profile, pve.ClientOptions) (pve.Backend, error)

type Dependencies struct {
	BackendFactory BackendFactory
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
}

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

type runtime struct {
	backend pve.Backend
	format  string
	tasks   pve.TaskRunner
	logger  *slog.Logger
	verbose bool
	stdout  io.Writer
	stderr  io.Writer
}

func NewApp(version string) *cli.App {
	return NewAppWithDependencies(version, Dependencies{})
}

func Run(args []string, version string) error {
	return RunWithDependencies(args, version, Dependencies{})
}

func RunWithDependencies(args []string, version string, deps Dependencies) error {
	return RunWithBuildInfoAndDependencies(args, BuildInfo{Version: version}, deps)
}

func RunWithBuildInfo(args []string, info BuildInfo) error {
	return RunWithBuildInfoAndDependencies(args, info, Dependencies{})
}

func RunWithBuildInfoAndDependencies(args []string, info BuildInfo, deps Dependencies) error {
	info = info.withDefaults()
	app := NewAppWithBuildInfoAndDependencies(info, deps)
	return app.Run(normalizeArgs(args))
}

func NewAppWithDependencies(version string, deps Dependencies) *cli.App {
	return NewAppWithBuildInfoAndDependencies(BuildInfo{Version: version}.withDefaults(), deps)
}

func NewAppWithBuildInfoAndDependencies(info BuildInfo, deps Dependencies) *cli.App {
	info = info.withDefaults()
	deps = deps.withDefaults()

	app := &cli.App{
		Name:                   "pvectl",
		Usage:                  "Personal HomeLab Proxmox VE CLI",
		Version:                info.Version,
		UseShortOptionHandling: true,
		Writer:                 deps.Stdout,
		ErrWriter:              deps.Stderr,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "config",
				Value: config.DefaultPath,
				Usage: "config file path",
			},
			&cli.StringFlag{
				Name:  "profile",
				Usage: "profile name",
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Usage:   "output format: table,json,yaml",
			},
			&cli.DurationFlag{
				Name:  "timeout",
				Usage: "PVE API request timeout",
			},
			&cli.BoolFlag{
				Name:  "wait",
				Usage: "wait for async task completion",
			},
			&cli.DurationFlag{
				Name:  "wait-timeout",
				Usage: "task wait timeout",
			},
			&cli.BoolFlag{
				Name:  "insecure",
				Usage: "skip TLS certificate verification",
			},
			&cli.BoolFlag{
				Name:  "verbose",
				Usage: "enable verbose logging",
			},
		},
		Commands: []*cli.Command{
			newVersionCommand(info),
			newConfigCommand(),
			newDoctorCommand(deps),
			newStatusCommand(deps),
			newCheckCommand(deps),
			newNodeCommand(deps),
			newGuestAggregateCommand(deps),
			newTaskCommand(deps),
			newBackupCommand(deps),
			newStorageCommand(deps),
			newNetworkCommand(deps),
			newFirewallCommand(deps),
			newVMCommand(deps),
			newLXCCommand(deps),
		},
	}
	return app
}

func (info BuildInfo) withDefaults() BuildInfo {
	if info.Version == "" {
		info.Version = "dev"
	}
	if info.Commit == "" {
		info.Commit = "unknown"
	}
	if info.Date == "" {
		info.Date = "unknown"
	}
	return info
}

func (info BuildInfo) versionInfo() output.VersionInfo {
	info = info.withDefaults()
	return output.VersionInfo{
		Version:   info.Version,
		Commit:    info.Commit,
		Date:      info.Date,
		GoVersion: goruntime.Version(),
		OS:        goruntime.GOOS,
		Arch:      goruntime.GOARCH,
	}
}

func (d Dependencies) withDefaults() Dependencies {
	if d.BackendFactory == nil {
		d.BackendFactory = pve.NewProxmoxBackend
	}
	if d.Stdin == nil {
		d.Stdin = os.Stdin
	}
	if d.Stdout == nil {
		d.Stdout = os.Stdout
	}
	if d.Stderr == nil {
		d.Stderr = os.Stderr
	}
	return d
}

func buildRuntime(c *cli.Context, deps Dependencies) (*runtime, error) {
	deps = deps.withDefaults()

	cfg, err := config.Load(c.String("config"))
	if err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}
	_, profileCfg, err := cfg.SelectProfile(c.String("profile"))
	if err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}
	secret, err := config.ResolveTokenSecret(profileCfg)
	if err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}

	apiTimeout, err := resolveAPIRequestTimeout(c, profileCfg)
	if err != nil {
		return nil, err
	}
	format, err := resolveOutputFormat(c, profileCfg)
	if err != nil {
		return nil, err
	}

	verbose := boolFlag(c, "verbose")
	logger := slog.New(slog.NewTextHandler(c.App.ErrWriter, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	backend, err := deps.BackendFactory(profileCfg, pve.ClientOptions{
		TokenSecret: secret,
		Timeout:     apiTimeout,
		Insecure:    boolFlag(c, "insecure"),
	})
	if err != nil {
		return nil, err
	}

	waitTimeout := durationFlag(c, "wait-timeout")
	if waitTimeout <= 0 {
		waitTimeout = 5 * time.Minute
	}

	return &runtime{
		backend: backend,
		format:  format,
		tasks: pve.TaskRunner{
			Wait:        boolFlag(c, "wait"),
			WaitTimeout: waitTimeout,
			ErrWriter:   c.App.ErrWriter,
		},
		logger:  logger,
		verbose: verbose,
		stdout:  c.App.Writer,
		stderr:  c.App.ErrWriter,
	}, nil
}

func resolveAPIRequestTimeout(c *cli.Context, profile config.Profile) (time.Duration, error) {
	if timeout := durationFlag(c, "timeout"); timeout > 0 {
		return timeout, nil
	}
	if profile.Timeout != "" {
		timeout, err := time.ParseDuration(profile.Timeout)
		if err != nil {
			return 0, fmt.Errorf("config error: invalid timeout %q: %w", profile.Timeout, err)
		}
		return timeout, nil
	}
	return 30 * time.Second, nil
}

func resolveOutputFormat(c *cli.Context, profile config.Profile) (string, error) {
	format := stringFlag(c, "output")
	if format == "" {
		format = profile.DefaultOutput
	}
	format = output.NormalizeFormat(format)
	if err := output.ValidateFormat(format); err != nil {
		return "", err
	}
	return format, nil
}

func stringFlag(c *cli.Context, name string) string {
	for _, ctx := range c.Lineage() {
		if ctx.IsSet(name) {
			return ctx.String(name)
		}
	}
	return c.String(name)
}

func boolFlag(c *cli.Context, name string) bool {
	for _, ctx := range c.Lineage() {
		if ctx.IsSet(name) {
			return ctx.Bool(name)
		}
	}
	return c.Bool(name)
}

func durationFlag(c *cli.Context, name string) time.Duration {
	for _, ctx := range c.Lineage() {
		if ctx.IsSet(name) {
			return ctx.Duration(name)
		}
	}
	return c.Duration(name)
}

func flagIsSet(c *cli.Context, name string) bool {
	for _, ctx := range c.Lineage() {
		if ctx.IsSet(name) {
			return true
		}
	}
	return false
}

func commonNodeFlag() []cli.Flag {
	return []cli.Flag{&cli.StringFlag{Name: "node", Usage: "PVE node name"}}
}

func commonOutputFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Usage: "output format: table,json,yaml"},
	}
}

func commonControlFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "node", Usage: "PVE node name"},
		&cli.BoolFlag{Name: "wait", Usage: "wait for async task completion"},
		&cli.DurationFlag{Name: "wait-timeout", Usage: "task wait timeout"},
	}
}
