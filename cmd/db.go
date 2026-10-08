package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dapasa/ssmgate/internal/config"
	"github.com/dapasa/ssmgate/internal/connector"
	"github.com/dapasa/ssmgate/internal/prereq"
	"github.com/dapasa/ssmgate/internal/ui"
)

// psqlRunner is implemented by connectors that provide their own client
// (the demo connector simulates psql instead of launching it).
type psqlRunner interface {
	Psql(ctx context.Context, env *config.Environment, t *connector.Tunnel) error
}

func newDBCmd(a *app) *cobra.Command {
	c := &cobra.Command{
		Use:   "db",
		Short: "Tunnel to the environment's database and connect with psql",
		Long: `Open an SSM port-forward to the environment's database through its tunnel
host, mint a 15-minute RDS IAM token (auth: iam) and launch psql. Closing psql
closes the tunnel. To keep the tunnel open for another client use "tunnel".`,
		Example: `  ssmgate db --env dev
  ssmgate --demo db --env dev`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, env, err := a.setup(c)
			if err != nil {
				return err
			}
			if env.DB == nil {
				return ui.Errorf("environment %q has no db section", env.Name)
			}
			conn := a.connector(cfg)
			runner, simulated := conn.(psqlRunner)
			// Fail before any login work if psql is missing.
			if !simulated {
				if err := prereq.CheckPsql(); err != nil {
					return err
				}
			}

			ui.Log("ssmgate db — environment: %s", env.Name)
			t, err := conn.OpenTunnel(c.Context(), env)
			if err != nil {
				return err
			}
			defer t.Close()

			ui.Log("Launching psql -> %s", PsqlConnString(env.DB, t))
			if simulated {
				return runner.Psql(c.Context(), env, t)
			}
			return runPsql(env.DB, t)
		},
	}
	a.addEnvFlags(c)
	return c
}

// PsqlConnString builds the libpq connection string for the local tunnel.
//
// Through a tunnel the server is reached as 127.0.0.1, so verify-full would
// fail the hostname check against the real certificate; verify-ca (the
// default) still validates the chain against the CA bundle.
func PsqlConnString(db *config.Database, t *connector.Tunnel) string {
	parts := []string{
		"host=127.0.0.1",
		fmt.Sprintf("port=%d", t.LocalPort),
		"dbname=" + quoteConnValue(db.Name),
	}
	if t.DBUser != "" {
		parts = append(parts, "user="+quoteConnValue(t.DBUser))
	}
	parts = append(parts, "sslmode="+db.SSLMode)
	if t.CAPath != "" {
		parts = append(parts, "sslrootcert="+quoteConnValue(t.CAPath))
	}
	return strings.Join(parts, " ")
}

// quoteConnValue quotes a libpq conninfo value when it has spaces or quotes
// (e.g. a Windows path under "Application Data").
func quoteConnValue(v string) string {
	if v != "" && !strings.ContainsAny(v, ` '\`) {
		return v
	}
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `'`, `\'`)
	return "'" + v + "'"
}

func runPsql(db *config.Database, t *connector.Tunnel) error {
	psql := exec.Command("psql", PsqlConnString(db, t))
	psql.Env = os.Environ()
	if t.Password != "" {
		psql.Env = append(psql.Env, "PGPASSWORD="+t.Password)
	}
	psql.Stdin, psql.Stdout, psql.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := psql.Run(); err != nil {
		return ui.Errorf("psql exited: %v", err)
	}
	return nil
}
