package cmd

import (
	"github.com/spf13/cobra"

	"github.com/dapasa/ssmgate/internal/prereq"
	"github.com/dapasa/ssmgate/internal/ui"
)

func newDoctorCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the local setup (config, session-manager-plugin, psql)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			problems := 0

			if cfg, err := a.loadConfig(); err != nil {
				problems++
				ui.Warn("config: %v", err)
			} else {
				src := a.resolvedConfigPath()
				if a.demo && a.configPath == "" {
					src = "built-in demo sample"
				}
				ui.OK("config: %s — %d environment(s) %v", src, len(cfg.Environments), cfg.EnvironmentNames())
			}

			if p, err := prereq.LookPath(prereq.PluginName); err != nil {
				if a.demo {
					ui.Warn("%s: not found (not needed in --demo). Install: %s", prereq.PluginName, prereq.PluginInstallURL)
				} else {
					problems++
					ui.Warn("%s: not found — required for every command. Install: %s", prereq.PluginName, prereq.PluginInstallURL)
				}
			} else {
				ui.OK("%s: %s", prereq.PluginName, p)
			}

			if p, err := prereq.LookPath("psql"); err != nil {
				ui.Warn("psql: not found — only needed by `db` (`tunnel` works without it)")
			} else {
				ui.OK("psql: %s", p)
			}

			if problems > 0 {
				return ui.Errorf("%d problem(s) found", problems)
			}
			ui.OK("All good.")
			return nil
		},
	}
}
