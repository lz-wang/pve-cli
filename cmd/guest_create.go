package cmd

import (
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/output"
	"github.com/lz-wang/pve-cli/v3/internal/pve"
	"github.com/urfave/cli/v2"
	"os"
)

func lxcCreateCommand(deps Dependencies) *cli.Command {
	return &cli.Command{Name: "create", Usage: "Create a stopped system container", ArgsUsage: "CTID", Flags: append(commonControlFlags(),
		&cli.StringFlag{Name: "template", Required: true}, &cli.StringFlag{Name: "rootfs", Required: true}, &cli.StringFlag{Name: "hostname"}, &cli.IntFlag{Name: "memory", Value: 1024}, &cli.IntFlag{Name: "cores", Value: 1}, &cli.IntFlag{Name: "swap", Value: 512}, &cli.StringFlag{Name: "net0"}, &cli.BoolFlag{Name: "unprivileged", Value: true}, &cli.StringFlag{Name: "ssh-key-file"}, &cli.StringFlag{Name: "password-env"},
	), Action: func(c *cli.Context) error {
		if err := requireNoExtraArgs(c, 1); err != nil {
			return err
		}
		id, err := parseVMID(c.Args().First())
		if err != nil {
			return err
		}
		keys := ""
		if path := c.String("ssh-key-file"); path != "" {
			data, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			keys = string(data)
		}
		password := ""
		if env := c.String("password-env"); env != "" {
			password = os.Getenv(env)
			if password == "" {
				return fmt.Errorf("invalid empty password environment variable")
			}
		}
		rt, err := buildRuntime(c, deps)
		if err != nil {
			return err
		}
		result, err := pve.NewCreateService(rt.backend, rt.tasks).CreateLXC(c.Context, pve.LXCCreateOptions{ID: id, Node: c.String("node"), Template: c.String("template"), RootFS: c.String("rootfs"), Hostname: c.String("hostname"), Memory: c.Int("memory"), Cores: c.Int("cores"), Swap: c.Int("swap"), Net0: c.String("net0"), Unprivileged: c.Bool("unprivileged"), SSHKeys: keys, Password: password})
		if result.Task != "" {
			if e := output.WriteOperation(rt.stdout, rt.format, result); e != nil {
				return e
			}
		}
		return err
	}}
}
