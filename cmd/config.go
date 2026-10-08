package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/dapasa/ssmgate/internal/config"
	"github.com/dapasa/ssmgate/internal/ui"
)

func newConfigCmd(a *app) *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Create, locate and validate the configuration file",
	}

	var force bool
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Write an annotated sample config to edit",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			path := a.resolvedConfigPath()
			if _, err := os.Stat(path); err == nil && !force {
				return ui.Errorf("%s already exists (use --force to overwrite)", path)
			}
			if err := writeSample(path); err != nil {
				return err
			}
			ui.OK("Wrote sample config to %s", path)
			ui.Log("Edit it with your SSO portal, accounts, instances and databases, then run: ssmgate config validate")
			return nil
		},
	}
	initCmd.Flags().BoolVar(&force, "force", false, "overwrite an existing file")

	pathCmd := &cobra.Command{
		Use:   "path",
		Short: "Print the config file path in use",
		Args:  cobra.NoArgs,
		Run: func(c *cobra.Command, _ []string) {
			_, _ = fmt.Fprintln(c.OutOrStdout(), a.resolvedConfigPath())
		},
	}

	validateCmd := &cobra.Command{
		Use:   "validate",
		Short: "Check the config file and report every problem found",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, err := a.loadConfig()
			if err != nil {
				return err
			}
			ui.OK("Config is valid: %d environment(s): %v", len(cfg.Environments), cfg.EnvironmentNames())
			return nil
		},
	}

	c.AddCommand(initCmd, pathCmd, validateCmd)
	return c
}

func writeSample(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return ui.Errorf("could not create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, config.Example, 0o600); err != nil {
		return ui.Errorf("could not write %s: %v", path, err)
	}
	return nil
}
