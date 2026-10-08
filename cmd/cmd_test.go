package cmd

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dapasa/ssmgate/internal/config"
	"github.com/dapasa/ssmgate/internal/connector"
	"github.com/dapasa/ssmgate/internal/demo"
	"github.com/dapasa/ssmgate/internal/ui"
)

// run executes the real command tree offline (demo connector, no delays)
// and returns stdout, stderr and the error.
func run(t *testing.T, ctx context.Context, stdin string, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut, log bytes.Buffer
	ui.Out = &log
	t.Cleanup(func() { ui.Out = os.Stderr })

	in := strings.NewReader(stdin) // shared, like a real terminal
	a := &app{
		in:        in,
		clipboard: func(string) string { return "clipboard disabled in tests" },
		newConnector: func(*config.File) connector.Connector {
			return &demo.Connector{In: in, Out: &out}
		},
	}
	root := newRootCmd("test", a)
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&errOut)
	err := root.ExecuteContext(ctx)
	return out.String(), errOut.String() + log.String(), err
}

// demoConfig writes the sample config with a free local port for dev.
func demoConfig(t *testing.T) (path string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port = ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	s := strings.Replace(string(config.Example), "local_port: 55432", "local_port: "+strconv.Itoa(port), 1)
	path = filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, port
}

func TestEnvsListsSampleEnvironments(t *testing.T) {
	out, _, err := run(t, context.Background(), "", "--demo", "envs")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dev", "prod", "sandbox", "profile:sandbox-admin", "appdb (iam, via bastion)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestEnvIsRequired(t *testing.T) {
	_, _, err := run(t, context.Background(), "", "--demo", "db")
	if err == nil || !strings.Contains(err.Error(), "--env is required") {
		t.Fatalf("got %v", err)
	}
}

func TestUnknownHost(t *testing.T) {
	_, _, err := run(t, context.Background(), "", "--demo", "shell", "nope", "--env", "dev")
	if err == nil || !strings.Contains(err.Error(), "Valid hosts: bastion, vpn") {
		t.Fatalf("got %v", err)
	}
}

func TestProtectedEnvironmentConfirmation(t *testing.T) {
	_, _, err := run(t, context.Background(), "staging\n", "--demo", "shell", "bastion", "--env", "prod")
	if err == nil || !strings.Contains(err.Error(), "did not match") {
		t.Fatalf("wrong confirmation must abort, got %v", err)
	}
	_, _, err = run(t, context.Background(), "", "--demo", "shell", "bastion", "--env", "prod")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("no input must abort and mention --yes, got %v", err)
	}
	out, _, err := run(t, context.Background(), "prod\nexit\n", "--demo", "shell", "bastion", "--env", "prod")
	if err != nil || !strings.Contains(out, "sh-5.2$") {
		t.Fatalf("typing the name must continue: %v\n%s", err, out)
	}
	if _, _, err := run(t, context.Background(), "exit\n", "--demo", "shell", "bastion", "--env", "prod", "--yes"); err != nil {
		t.Fatalf("--yes must skip confirmation: %v", err)
	}
}

func TestShellDemo(t *testing.T) {
	out, log, err := run(t, context.Background(), "whoami\nexit\n", "--demo", "shell", "vpn", "--env", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ssm-user") || !strings.Contains(log, "Assumed role VpnSessionOperator") {
		t.Errorf("out:\n%s\nlog:\n%s", out, log)
	}
}

func TestTunnelBlocksUntilCancelled(t *testing.T) {
	path, port := demoConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	out, _, err := run(t, ctx, "", "--demo", "--config", path, "tunnel", "--env", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 250*time.Millisecond {
		t.Error("tunnel must block until the context is cancelled")
	}
	for _, want := range []string{"Port      : " + strconv.Itoa(port), "IAM auth token", "PGPASSWORD=", "sslmode=verify-ca"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestDBDemo(t *testing.T) {
	path, _ := demoConfig(t)
	out, _, err := run(t, context.Background(), "select current_user;\n\\q\n", "--demo", "--config", path, "db", "--env", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "demo.user") {
		t.Errorf("out:\n%s", out)
	}
}

func TestConfigInitAndValidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	if _, _, err := run(t, context.Background(), "", "--config", path, "config", "init"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, context.Background(), "", "--config", path, "config", "init"); err == nil {
		t.Error("init must refuse to overwrite without --force")
	}
	if _, _, err := run(t, context.Background(), "", "--config", path, "config", "init", "--force"); err != nil {
		t.Errorf("--force: %v", err)
	}
	if _, log, err := run(t, context.Background(), "", "--config", path, "config", "validate"); err != nil || !strings.Contains(log, "Config is valid") {
		t.Errorf("validate: %v %s", err, log)
	}
	out, _, _ := run(t, context.Background(), "", "--config", path, "config", "path")
	if strings.TrimSpace(out) != path {
		t.Errorf("config path = %q", out)
	}

	if err := os.WriteFile(path, []byte("environments: {dev: {region: x}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, context.Background(), "", "--config", path, "config", "validate"); err == nil {
		t.Error("invalid config must fail validation")
	}
}

func TestPsqlConnString(t *testing.T) {
	db := &config.Database{Name: "app", SSLMode: "verify-ca"}
	tun := &connector.Tunnel{LocalPort: 55432, DBUser: "alice", CAPath: `C:\Users\Me Too\rds.pem`}
	got := PsqlConnString(db, tun)
	want := `host=127.0.0.1 port=55432 dbname=app user=alice sslmode=verify-ca sslrootcert='C:\\Users\\Me Too\\rds.pem'`
	if got != want {
		t.Errorf("\ngot  %s\nwant %s", got, want)
	}

	db.SSLMode = "require"
	got = PsqlConnString(db, &connector.Tunnel{LocalPort: 5000})
	if got != "host=127.0.0.1 port=5000 dbname=app sslmode=require" {
		t.Errorf("minimal conn string = %s", got)
	}
}

func TestQuoteConnValue(t *testing.T) {
	for in, want := range map[string]string{
		"plain":  "plain",
		"":       "''",
		"a b":    "'a b'",
		"it's":   `'it\'s'`,
		`c:\x\y`: `'c:\\x\\y'`,
	} {
		if got := quoteConnValue(in); got != want {
			t.Errorf("quoteConnValue(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestReadLineLeavesRestOfInput(t *testing.T) {
	r := strings.NewReader("prod\nnext line\n")
	if line, err := readLine(r); err != nil || line != "prod" {
		t.Fatalf("got %q %v", line, err)
	}
	rest, _ := io.ReadAll(r)
	if string(rest) != "next line\n" {
		t.Errorf("rest = %q", rest)
	}
}
