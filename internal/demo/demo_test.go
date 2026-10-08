package demo

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/dapasa/ssmgate/internal/config"
	"github.com/dapasa/ssmgate/internal/prereq"
	"github.com/dapasa/ssmgate/internal/ui"
)

func init() { ui.Out = io.Discard }

// freePort returns a local port that is currently free.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func env(t *testing.T, name string) *config.Environment {
	t.Helper()
	f, err := config.Parse(config.Example)
	if err != nil {
		t.Fatal(err)
	}
	e := f.Environments[name]
	e.DB.LocalPort = freePort(t)
	return e
}

func TestOpenTunnelListensAndCloses(t *testing.T) {
	e := env(t, "dev")
	c := &Connector{In: strings.NewReader(""), Out: io.Discard}

	tun, err := c.OpenTunnel(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if !prereq.PortOpen(e.DB.LocalPort) {
		t.Fatal("demo tunnel must listen on the local port")
	}
	if tun.DBUser != "demo.user" || !strings.Contains(tun.Password, "DBUser=demo.user") || tun.CAPath == "" {
		t.Errorf("unexpected tunnel %+v", tun)
	}

	tun.Close()
	select {
	case <-tun.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done not closed after Close")
	}
	if prereq.PortOpen(e.DB.LocalPort) {
		t.Error("port still open after Close")
	}
	tun.Close() // idempotent
}

func TestOpenTunnelPasswordAuth(t *testing.T) {
	e := env(t, "sandbox")
	tun, err := (&Connector{}).OpenTunnel(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	defer tun.Close()
	if tun.Password != "" || tun.DBUser != "postgres" || tun.CAPath != "" {
		t.Errorf("password-auth tunnel = %+v", tun)
	}
}

func TestOpenTunnelRefusesBusyPort(t *testing.T) {
	e := env(t, "dev")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	e.DB.LocalPort = ln.Addr().(*net.TCPAddr).Port

	if _, err := (&Connector{}).OpenTunnel(context.Background(), e); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("expected busy-port error, got %v", err)
	}
}

func TestShellREPL(t *testing.T) {
	var out bytes.Buffer
	c := &Connector{In: strings.NewReader("whoami\nhostname\nrm -rf /\nexit\nnever-run\n"), Out: &out}
	if err := c.Shell(context.Background(), env(t, "dev"), "vpn"); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"ssm-user", "dev-vpn.internal", "rm: command simulated"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in output:\n%s", want, s)
		}
	}
	if strings.Contains(s, "never-run") {
		t.Error("input after exit must not be processed")
	}
}

func TestPsqlREPL(t *testing.T) {
	e := env(t, "dev")
	c := &Connector{In: strings.NewReader("select current_user;\n\\conninfo\n\\q\n"), Out: io.Discard}
	tun, err := c.OpenTunnel(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	defer tun.Close()

	var out bytes.Buffer
	c.Out = &out
	if err := c.Psql(context.Background(), e, tun); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "demo.user") || !strings.Contains(out.String(), `database "appdb"`) {
		t.Errorf("psql output:\n%s", out.String())
	}
}

func TestREPLStopsOnContextCancel(t *testing.T) {
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := repl(ctx, pr, io.Discard, "> ", func(string) string { return "" }); err != nil {
		t.Fatal(err)
	}
}
