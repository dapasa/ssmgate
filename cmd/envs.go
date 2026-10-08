package cmd

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newEnvsCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "envs",
		Short: "List the configured environments, hosts and databases",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := a.loadConfig()
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(c.OutOrStdout(), 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ENV\tAUTH\tREGION\tHOSTS\tDATABASE\tLOCAL PORT\tCONFIRM")
			for _, name := range cfg.EnvironmentNames() {
				env := cfg.Environments[name]
				auth := "sso:" + env.AccountID
				if !env.UsesSSO() {
					auth = "profile:" + env.Profile
				}
				db, port := "-", "-"
				if env.DB != nil {
					db = fmt.Sprintf("%s (%s, via %s)", env.DB.Name, env.DB.Auth, env.DB.Via)
					port = fmt.Sprint(env.DB.LocalPort)
				}
				confirm := ""
				if env.Confirm {
					confirm = "yes"
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					name, auth, env.Region, strings.Join(env.HostNames(), ","), db, port, confirm)
			}
			return w.Flush()
		},
	}
}
