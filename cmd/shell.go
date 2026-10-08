package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/dapasa/ssmgate/internal/ui"
)

func newShellCmd(a *app) *cobra.Command {
	c := &cobra.Command{
		Use:   "shell <host>",
		Short: "Open an interactive SSM shell on a host",
		Long: `Open an interactive SSM shell (account-default session document) on one of
the environment's hosts. The SSO role is per host, as declared in the config.`,
		Example: `  ssmgate shell bastion --env dev
  ssmgate --demo shell vpn --env dev`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			cfg, env, err := a.setup(c)
			if err != nil {
				return err
			}
			host := args[0]
			if _, ok := env.Hosts[host]; !ok {
				return ui.Errorf("unknown host %q in %s. Valid hosts: %s", host, env.Name, strings.Join(env.HostNames(), ", "))
			}
			ui.Log("ssmgate shell — environment: %s, host: %s", env.Name, host)
			return a.connector(cfg).Shell(c.Context(), env, host)
		},
	}
	a.addEnvFlags(c)
	return c
}
