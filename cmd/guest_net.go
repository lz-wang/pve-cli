package cmd

import (
	"github.com/urfave/cli/v2"

	"github.com/lz-wang/pve-cli/v3/internal/pve"
)

// newLXCNetCommand exposes safe partial updates of LXC network devices.
// Unlike `config set`, which assigns whole native fields, `net set` reads the
// complete device configuration and modifies only the requested attributes.
func newLXCNetCommand(deps Dependencies) *cli.Command {
	set := &cli.Command{
		Name:      "set",
		Usage:     "Safely update one LXC network device attribute-wise",
		ArgsUsage: "VMID",
		Flags: append(commonControlFlags(),
			&cli.StringFlag{Name: "device", Usage: "network device slot (net0, net1, ...) **Required.**"},
			&cli.StringFlag{Name: "bridge", Usage: "set bridge= attribute after verifying it exists on the node"},
			&cli.StringFlag{Name: "iface", Usage: "set name= attribute (interface name inside the container)"},
			&cli.StringFlag{Name: "ip", Usage: "set ip= attribute (dhcp or CIDR)"},
			&cli.StringFlag{Name: "gw", Usage: "set gw= attribute (gateway address)"},
			&cli.StringFlag{Name: "tag", Usage: "set tag= attribute (VLAN tag)"},
			&cli.StringFlag{Name: "rate", Usage: "set rate= attribute (MB/s limit)"},
		),
		Action: func(c *cli.Context) error {
			if err := requireNoExtraArgs(c, 1); err != nil {
				return err
			}
			options := pve.LXCNetUpdateOptions{
				Node:    c.String("node"),
				Device:  c.String("device"),
				Bridge:  c.String("bridge"),
				Name:    c.String("iface"),
				IP:      c.String("ip"),
				Gateway: c.String("gw"),
				Tag:     c.String("tag"),
				Rate:    c.String("rate"),
			}
			id, err := resolveGuestID(c, "lxc", deps, c.Args().First())
			if err != nil {
				return err
			}
			rt, err := buildRuntime(c, deps)
			if err != nil {
				return err
			}
			rt.tasks.ResultWriter = rt.stdout
			return guestService("lxc", rt).UpdateLXCNet(c.Context, id, options)
		},
	}
	return &cli.Command{
		Name:        "net",
		Usage:       "Safely update LXC network device configuration",
		Subcommands: []*cli.Command{set},
	}
}
