// Package cmd wires the ssmgate CLI (cobra).
package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dapasa/ssmgate/internal/awsx"
	"github.com/dapasa/ssmgate/internal/config"
	"github.com/dapasa/ssmgate/internal/connector"
	"github.com/dapasa/ssmgate/internal/demo"
	"github.com/dapasa/ssmgate/internal/ui"
)

// EnvDemo enables demo mode like --demo (any non-empty value).
const EnvDemo = "SSMGATE_DEMO"

// app holds the global flags and the injectable dependencies of every
// command, so tests can run the real command tree offline.
type app struct {
	configPath string
	demo       bool
	envName    string
	yes        bool

	in           io.Reader
	newConnector func(cfg *config.File) connector.Connector
	clipboard    func(text string) string
}

// NewRootCmd builds the root command tree.
func NewRootCmd(version string) *cobra.Command {
	return newRootCmd(version, &app{in: os.Stdin, clipboard: copyToClipboard})
}

func newRootCmd(version string, a *app) *cobra.Command {
	root := &cobra.Command{
		Use:   "ssmgate",
		Short: "Reach private AWS hosts and databases over SSM — no SSH, no open ports",
		Long: `ssmgate — secure access to private AWS infrastructure over SSM Session Manager.

No SSH keys, no bastion port 22, no VPN: every session goes through AWS Systems
Manager and is authenticated with your IAM Identity Center (SSO) identity or an
AWS profile, so it is logged and attributable.

  shell   interactive shell on any SSM-managed instance
  db      tunnel to a private database (RDS) + 15-min IAM token + psql
  tunnel  same tunnel + token, kept open for your own client (DBeaver, ...)

Everything (accounts, instances, roles, databases) comes from a YAML config
file — run "ssmgate config init" to create one, or add --demo to try every
command offline with a built-in sample configuration.`,
		Example: `  ssmgate --demo envs                 # try it without AWS
  ssmgate config init                 # write a sample config to edit
  ssmgate shell bastion --env dev     # interactive shell on a host
  ssmgate db --env dev                # tunnel + psql
  ssmgate tunnel --env prod           # tunnel for a GUI client`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVarP(&a.configPath, "config", "c", "", "config file (default "+config.DefaultPath()+")")
	root.PersistentFlags().BoolVar(&a.demo, "demo", os.Getenv(EnvDemo) != "", "simulate AWS with a built-in sample config (no account needed)")

	root.AddCommand(newShellCmd(a), newDBCmd(a), newTunnelCmd(a), newEnvsCmd(a), newConfigCmd(a), newDoctorCmd(a))
	return root
}

// addEnvFlags registers the flags shared by commands that act on an
// environment. --env has no default on purpose: the target environment
// (possibly production) must always be chosen explicitly.
func (a *app) addEnvFlags(c *cobra.Command) {
	c.Flags().StringVarP(&a.envName, "env", "e", "", "target environment from the config (REQUIRED)")
	c.Flags().BoolVarP(&a.yes, "yes", "y", false, "skip the confirmation of environments marked confirm: true")
}

// resolvedConfigPath is the path the config is read from.
func (a *app) resolvedConfigPath() string {
	if a.configPath != "" {
		return a.configPath
	}
	return config.DefaultPath()
}

// loadConfig reads the config file, or the built-in sample in demo mode when
// no --config was given.
func (a *app) loadConfig() (*config.File, error) {
	var (
		cfg *config.File
		err error
	)
	if a.demo && a.configPath == "" {
		cfg, err = config.Parse(config.Example)
	} else {
		cfg, err = config.Load(a.resolvedConfigPath())
	}
	if err != nil {
		return nil, ui.Errorf("%v", err)
	}
	return cfg, nil
}

func (a *app) connector(cfg *config.File) connector.Connector {
	if a.newConnector != nil {
		return a.newConnector(cfg)
	}
	if a.demo {
		return demo.New()
	}
	return awsx.New(cfg.SSO)
}

// setup loads the config, resolves --env and asks for confirmation when the
// environment requires it.
func (a *app) setup(c *cobra.Command) (*config.File, *config.Environment, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, nil, err
	}
	env, err := cfg.Environment(a.envName)
	if err != nil {
		return nil, nil, ui.Errorf("%v", err)
	}
	if env.Confirm && !a.yes {
		if err := confirmEnv(a.in, c.ErrOrStderr(), env.Name); err != nil {
			return nil, nil, err
		}
	}
	return cfg, env, nil
}

// confirmEnv asks the user to type the environment name.
func confirmEnv(in io.Reader, out io.Writer, name string) error {
	_, _ = fmt.Fprintf(out, "\n  Environment %q is protected. Type its name to continue: ", name)
	line, err := readLine(in)
	if err != nil && line == "" {
		return ui.Errorf("confirmation required for %q (use --yes in non-interactive runs)", name)
	}
	if strings.TrimSpace(line) != name {
		return ui.Errorf("confirmation did not match %q — aborted.", name)
	}
	return nil
}

// readLine reads one line byte by byte, so nothing after the newline is
// consumed (the rest of stdin still belongs to the shell / psql session).
func readLine(in io.Reader) (string, error) {
	var sb strings.Builder
	b := make([]byte, 1)
	for {
		n, err := in.Read(b)
		if n == 1 {
			if b[0] == '\n' {
				return sb.String(), nil
			}
			sb.WriteByte(b[0])
		}
		if err != nil {
			return sb.String(), err
		}
	}
}
