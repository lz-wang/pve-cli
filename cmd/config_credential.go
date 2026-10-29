package cmd

import (
	"fmt"
	"github.com/lz-wang/pve-cli/v3/internal/config"
	"github.com/urfave/cli/v2"
	"io"
	"strings"
)

func configCredentialSource(c *cli.Context, deps Dependencies) (string, error) {
	count := 0
	for _, set := range []bool{c.String("token-secret-file") != "", c.String("token-secret-env") != "", c.Bool("token-secret-stdin")} {
		if set {
			count++
		}
	}
	if count != 1 {
		return "", fmt.Errorf("token-secret-file or token-secret-env is required; choose one source (or --token-secret-stdin)")
	}
	if c.Bool("token-secret-stdin") {
		data, err := io.ReadAll(io.LimitReader(deps.withDefaults().Stdin, 65537))
		if err != nil {
			return "", err
		}
		if len(data) > 65536 {
			return "", fmt.Errorf("token secret is too large")
		}
		return config.StoreCredential(c.String("config"), strings.TrimSpace(string(data)))
	}
	if path := c.String("token-secret-file"); path != "" {
		if _, err := config.ReadCredential(path); err != nil {
			return "", err
		}
		return config.ExpandPath(path)
	}
	return "", nil
}
