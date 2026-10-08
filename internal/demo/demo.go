// Package demo is an offline implementation of connector.Connector. It
// replays the same steps as the AWS backend (login, role, tunnel, token)
// without any network call, opens a real local TCP listener for the tunnel,
// and provides a small simulated shell and psql prompt. It lets anyone try
// every ssmgate command without an AWS account.
package demo

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/dapasa/ssmgate/internal/config"
	"github.com/dapasa/ssmgate/internal/connector"
	"github.com/dapasa/ssmgate/internal/prereq"
	"github.com/dapasa/ssmgate/internal/ui"
)

// User is the identity the demo pretends to be logged in as.
const User = "demo.user@example.com"

// Connector simulates AWS. In and Out are the terminal for simulated
// interactive sessions; Step is the pause between simulated steps.
type Connector struct {
	In   io.Reader
	Out  io.Writer
	Step time.Duration
}

var _ connector.Connector = (*Connector)(nil)

// New returns a demo connector bound to the process terminal.
func New() *Connector {
	return &Connector{In: os.Stdin, Out: os.Stdout, Step: 350 * time.Millisecond}
}

func (c *Connector) pause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(c.Step):
		return nil
	}
}

// login simulates the SSO / profile credential step.
func (c *Connector) login(ctx context.Context, env *config.Environment, role string) error {
	ui.Warn("DEMO MODE — no AWS call is made; every step below is simulated.")
	if !env.UsesSSO() {
		ui.OK("Using AWS profile %s.", env.Profile)
		return c.pause(ctx)
	}
	ui.Log("SSO device authorization: open https://device.sso.example.com and confirm code DEMO-1234")
	if err := c.pause(ctx); err != nil {
		return err
	}
	ui.OK("SSO login complete (%s).", User)
	if err := c.pause(ctx); err != nil {
		return err
	}
	ui.OK("Assumed role %s in account %s.", role, env.AccountID)
	return nil
}

// Shell simulates an interactive SSM shell on the host.
func (c *Connector) Shell(ctx context.Context, env *config.Environment, host string) error {
	h := env.Hosts[host]
	if err := c.login(ctx, env, h.Role); err != nil {
		return err
	}
	ui.Log("Opening interactive SSM shell on %q (%s) in %s...", host, h.InstanceID, env.Name)
	if err := c.pause(ctx); err != nil {
		return err
	}
	ui.OK("Dropping into the (simulated) remote shell. Type 'exit' to leave.")
	_, _ = fmt.Fprintf(c.Out, "\nStarting session with SessionId: %s-0demo\n", strings.Split(User, "@")[0])
	return repl(ctx, c.In, c.Out, "sh-5.2$ ", shellAnswer(env, host))
}

// OpenTunnel simulates the port-forward and opens a real listener on the
// configured local port, so port checks and GUI clients see it as open.
func (c *Connector) OpenTunnel(ctx context.Context, env *config.Environment) (*connector.Tunnel, error) {
	db := env.DB
	if err := prereq.EnsurePortFree(db.LocalPort); err != nil {
		return nil, err
	}
	if err := c.login(ctx, env, db.Role); err != nil {
		return nil, err
	}
	target := env.TunnelTarget()
	ui.Log("Opening SSM port-forward: localhost:%d -> %s:%d (via %s / %s)",
		db.LocalPort, db.Endpoint, db.Port, db.Via, target.InstanceID)
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", db.LocalPort))
	if err != nil {
		return nil, ui.Errorf("could not listen on localhost:%d: %v", db.LocalPort, err)
	}
	t := connector.NewTunnel(func() {
		_ = ln.Close()
		ui.OK("Tunnel closed.")
	})
	go serve(ln, t)
	if err := c.pause(ctx); err != nil {
		t.Close()
		return nil, err
	}
	ui.OK("Tunnel is up on localhost:%d.", db.LocalPort)

	t.LocalPort = db.LocalPort
	t.DBUser = db.User
	if t.DBUser == "" {
		t.DBUser = strings.Split(User, "@")[0]
	}
	if db.NeedsCABundle() {
		t.CAPath = db.CABundle
		if t.CAPath == "" {
			t.CAPath = config.RDSCABundlePath(env.Region)
		}
	}
	if db.Auth == config.AuthIAM {
		ui.Log("Generating 15-min RDS IAM auth token for user %q...", t.DBUser)
		t.Password = fakeToken(db, t.DBUser)
		ui.OK("Token generated (valid ~15 minutes).")
	}
	return t, nil
}

// serve accepts connections on the demo tunnel and answers with a notice, so
// a client pointed at it fails fast with a clear message instead of hanging.
func serve(ln net.Listener, t *connector.Tunnel) {
	defer t.MarkDone()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte("ssmgate demo tunnel: there is no real database behind this port\n"))
		_ = conn.Close()
	}
}

// fakeToken returns something shaped like an RDS IAM token (a presigned URL).
func fakeToken(db *config.Database, user string) string {
	return fmt.Sprintf("%s:%d/?Action=connect&DBUser=%s&X-Amz-Algorithm=AWS4-HMAC-SHA256"+
		"&X-Amz-Credential=DEMOAKIAEXAMPLE&X-Amz-Expires=900&X-Amz-Signature=demo0000000000000000",
		db.Endpoint, db.Port, user)
}

// Psql simulates an interactive psql session against the demo tunnel.
func (c *Connector) Psql(ctx context.Context, env *config.Environment, t *connector.Tunnel) error {
	_, _ = fmt.Fprintf(c.Out, "psql (16.4, server 16.3) [simulated]\nSSL connection (protocol: TLSv1.3)\nType \"help\" for help.\n\n")
	return repl(ctx, c.In, c.Out, env.DB.Name+"=> ", psqlAnswer(env, t))
}

// repl runs a tiny read-eval-print loop until "exit", "\q", EOF or ctx ends.
func repl(ctx context.Context, in io.Reader, out io.Writer, prompt string, answer func(string) string) error {
	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(in)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	for {
		_, _ = fmt.Fprint(out, prompt)
		select {
		case <-ctx.Done():
			_, _ = fmt.Fprintln(out)
			return nil
		case line, ok := <-lines:
			if !ok {
				_, _ = fmt.Fprintln(out)
				return nil
			}
			cmd := strings.TrimSpace(line)
			switch cmd {
			case "exit", "\\q", "quit":
				return nil
			case "":
				continue
			}
			_, _ = fmt.Fprintln(out, answer(cmd))
		}
	}
}

func shellAnswer(env *config.Environment, host string) func(string) string {
	return func(cmd string) string {
		switch strings.Fields(cmd)[0] {
		case "whoami":
			return "ssm-user"
		case "hostname":
			return fmt.Sprintf("%s-%s.internal", env.Name, host)
		case "uptime":
			return " 10:42:07 up 12 days,  3:14,  1 user,  load average: 0.02, 0.05, 0.01"
		case "pwd":
			return "/usr/bin"
		case "help":
			return "simulated shell — try: whoami, hostname, uptime, pwd, exit"
		default:
			return "sh: " + strings.Fields(cmd)[0] + ": command simulated in demo mode (try: help)"
		}
	}
}

func psqlAnswer(env *config.Environment, t *connector.Tunnel) func(string) string {
	return func(cmd string) string {
		q := strings.ToLower(strings.TrimSuffix(cmd, ";"))
		switch q {
		case "select current_user":
			return fmt.Sprintf(" current_user\n--------------\n %s\n(1 row)\n", t.DBUser)
		case "select version()":
			return " version\n---------------------------------\n PostgreSQL 16.3 (simulated)\n(1 row)\n"
		case "\\conninfo":
			return fmt.Sprintf("You are connected to database %q as user %q on host \"127.0.0.1\" at port %q.",
				env.DB.Name, t.DBUser, fmt.Sprint(t.LocalPort))
		case "\\dt":
			return "         List of relations\n Schema |  Name  | Type  | Owner\n--------+--------+-------+-------\n public | users  | table | app\n public | orders | table | app\n(2 rows)\n"
		case "help":
			return "simulated psql — try: select current_user; select version(); \\conninfo; \\dt; \\q"
		default:
			return "(demo) query not simulated — try: help"
		}
	}
}
