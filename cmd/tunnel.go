package cmd

import (
	"fmt"
	"io"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/dapasa/ssmgate/internal/config"
	"github.com/dapasa/ssmgate/internal/connector"
	"github.com/dapasa/ssmgate/internal/ui"
)

func newTunnelCmd(a *app) *cobra.Command {
	c := &cobra.Command{
		Use:   "tunnel",
		Short: "Open the database tunnel and keep it up for your own client",
		Long: `Open the SSM port-forward to the environment's database and keep it alive
until Ctrl-C, WITHOUT launching psql. Connection details are printed and the
IAM token (auth: iam) is copied to the clipboard when possible, ready to paste
as the password in DBeaver, TablePlus, psql, migration tools, etc.`,
		Example: `  ssmgate tunnel --env dev
  ssmgate --demo tunnel --env dev`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cfg, env, err := a.setup(c)
			if err != nil {
				return err
			}
			if env.DB == nil {
				return ui.Errorf("environment %q has no db section", env.Name)
			}
			ui.Log("ssmgate tunnel — environment: %s", env.Name)
			t, err := a.connector(cfg).OpenTunnel(c.Context(), env)
			if err != nil {
				return err
			}
			defer t.Close()

			clip := "auth: password — use your database password"
			if t.Password != "" {
				clip = a.clipboard(t.Password)
			}
			printTunnel(c.OutOrStdout(), env.DB, t, clip)

			// Block until Ctrl-C (cancels the context) or the tunnel dies.
			select {
			case <-c.Context().Done():
			case <-t.Done():
				return ui.Errorf("the tunnel closed unexpectedly")
			}
			return nil
		},
	}
	a.addEnvFlags(c)
	return c
}

func printTunnel(w io.Writer, db *config.Database, t *connector.Tunnel, clipStatus string) {
	password := "(the IAM token below — valid ~15 min, re-run to get a new one)"
	if t.Password == "" {
		password = "(your database password)"
	}
	user, root := t.DBUser, t.CAPath
	if user == "" {
		user = "(your database user)"
	}
	if root == "" {
		root = "-"
	}
	_, _ = fmt.Fprintf(w, `
  =====================================================================
  TUNNEL MODE — the tunnel stays up until you press Ctrl-C.
  Point your client (DBeaver, TablePlus, psql, ...) at:

     Host      : localhost
     Port      : %d
     Database  : %s
     User      : %s
     Password  : %s
     SSL mode  : %s
     Root cert : %s
`, t.LocalPort, db.Name, user, password, db.SSLMode, root)

	if t.Password != "" {
		_, _ = fmt.Fprintf(w, `
  IAM auth token (%s):
  ---------------------------------------------------------------------
%s
  ---------------------------------------------------------------------

  Ready-to-paste psql command (bash/zsh, token inlined):
     PGPASSWORD='%s' psql "%s"
`, clipStatus, t.Password, t.Password, PsqlConnString(db, t))
	} else {
		_, _ = fmt.Fprintf(w, "\n  psql command:\n     psql \"%s\"\n", PsqlConnString(db, t))
	}
	_, _ = fmt.Fprint(w, `
  Press Ctrl-C to close the tunnel.
  =====================================================================

`)
}

// copyToClipboard best-effort copies text to the OS clipboard and returns a
// human-readable status.
func copyToClipboard(text string) string {
	tools := [][]string{
		{"clip.exe"},                         // Windows / WSL
		{"pbcopy"},                           // macOS
		{"wl-copy"},                          // Wayland
		{"xclip", "-selection", "clipboard"}, // X11
	}
	for _, t := range tools {
		if _, err := exec.LookPath(t[0]); err != nil {
			continue
		}
		c := exec.Command(t[0], t[1:]...)
		stdin, err := c.StdinPipe()
		if err != nil {
			continue
		}
		if err := c.Start(); err != nil {
			continue
		}
		_, _ = stdin.Write([]byte(text))
		_ = stdin.Close()
		if err := c.Wait(); err == nil {
			return "copied to clipboard with " + t[0]
		}
	}
	return "no clipboard tool found — copy it manually"
}
