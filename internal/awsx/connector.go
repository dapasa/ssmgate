// Package awsx is the real AWS implementation of connector.Connector: SSO
// (IAM Identity Center) or shared-profile credentials, SSM Session Manager
// sessions via the session-manager-plugin, and RDS IAM auth tokens.
package awsx

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"

	"github.com/dapasa/ssmgate/internal/config"
	"github.com/dapasa/ssmgate/internal/connector"
	"github.com/dapasa/ssmgate/internal/prereq"
	"github.com/dapasa/ssmgate/internal/ui"
)

// TunnelTimeout is how long to wait for the local port to start listening.
const TunnelTimeout = 30 * time.Second

// Connector reaches real AWS infrastructure.
type Connector struct {
	SSO config.SSO
}

var _ connector.Connector = (*Connector)(nil)

// New returns an AWS connector for the given SSO portal settings.
func New(s config.SSO) *Connector { return &Connector{SSO: s} }

// awsConfig returns an aws.Config for the environment, using either the SSO
// role (account_id + role) or the environment's shared-config profile.
func (c *Connector) awsConfig(ctx context.Context, env *config.Environment, role string) (aws.Config, error) {
	if !env.UsesSSO() {
		cfg, err := awsconfig.LoadDefaultConfig(ctx,
			awsconfig.WithRegion(env.Region),
			awsconfig.WithSharedConfigProfile(env.Profile),
		)
		if err != nil {
			return aws.Config{}, ui.Errorf("could not load AWS profile %q: %v", env.Profile, err)
		}
		if _, err := cfg.Credentials.Retrieve(ctx); err != nil {
			return aws.Config{}, ui.Errorf("AWS profile %q has no valid credentials: %v\n"+
				"     (for an SSO profile run: aws sso login --profile %s)", env.Profile, err, env.Profile)
		}
		ui.OK("Using AWS profile %s.", env.Profile)
		return cfg, nil
	}

	token, err := Login(ctx, c.SSO)
	if err != nil {
		return aws.Config{}, err
	}
	creds, err := RoleCredentials(ctx, c.SSO, token, env.AccountID, role)
	if err != nil {
		return aws.Config{}, err
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(env.Region),
		awsconfig.WithCredentialsProvider(creds),
	)
	if err != nil {
		return aws.Config{}, ui.Errorf("failed to init AWS config: %v", err)
	}
	return cfg, nil
}

// Shell opens an interactive SSM shell on the named host.
func (c *Connector) Shell(ctx context.Context, env *config.Environment, host string) error {
	h := env.Hosts[host]
	if err := prereq.CheckSessionManagerPlugin(); err != nil {
		return err
	}
	cfg, err := c.awsConfig(ctx, env, h.Role)
	if err != nil {
		return err
	}
	ui.Log("Opening interactive SSM shell on %q (%s) in %s...", host, h.InstanceID, env.Name)
	ui.OK("Dropping into the remote shell. Type 'exit' to leave.")
	return startShell(ctx, cfg, h.InstanceID, roleHint(env, h.Role))
}

// OpenTunnel opens the SSM port-forward to the environment's database and
// prepares the credentials a client needs. On error nothing is left running.
func (c *Connector) OpenTunnel(ctx context.Context, env *config.Environment) (*connector.Tunnel, error) {
	db := env.DB
	if err := prereq.CheckSessionManagerPlugin(); err != nil {
		return nil, err
	}
	if err := prereq.EnsurePortFree(db.LocalPort); err != nil {
		return nil, err
	}
	cfg, err := c.awsConfig(ctx, env, db.Role)
	if err != nil {
		return nil, err
	}

	caPath, err := ensureCA(ctx, env)
	if err != nil {
		return nil, err
	}

	dbUser := db.User
	if v := os.Getenv(EnvDBUser); v != "" {
		dbUser = v
	}
	if db.Auth == config.AuthIAM {
		if dbUser, err = resolveDBUser(ctx, cfg, db); err != nil {
			return nil, err
		}
	}

	target := env.TunnelTarget()
	ui.Log("Opening SSM port-forward: localhost:%d -> %s:%d (via %s / %s)",
		db.LocalPort, db.Endpoint, db.Port, db.Via, target.InstanceID)
	proc, err := startPortForward(ctx, cfg, target.InstanceID, db.Endpoint, db.Port, db.LocalPort)
	if err != nil {
		return nil, err
	}

	var t *connector.Tunnel
	t = connector.NewTunnel(func() { stopProcess(proc, t.Done()) })
	go func() { _ = proc.Wait(); t.MarkDone() }()

	if err := waitForPort(t, db.LocalPort, TunnelTimeout); err != nil {
		t.Close()
		return nil, err
	}
	ui.OK("Tunnel is up on localhost:%d.", db.LocalPort)

	t.LocalPort, t.DBUser, t.CAPath = db.LocalPort, dbUser, caPath
	if db.Auth == config.AuthIAM {
		if t.Password, err = buildAuthToken(ctx, cfg, db, dbUser); err != nil {
			t.Close()
			return nil, err
		}
	}
	return t, nil
}

// ensureCA returns the root certificate path for verify-* SSL modes,
// downloading the AWS RDS regional bundle when none is configured.
func ensureCA(ctx context.Context, env *config.Environment) (string, error) {
	db := env.DB
	if !db.NeedsCABundle() {
		return "", nil
	}
	if db.CABundle != "" {
		if _, err := os.Stat(db.CABundle); err != nil {
			return "", ui.Errorf("db.ca_bundle %s: %v", db.CABundle, err)
		}
		return db.CABundle, nil
	}
	path := config.RDSCABundlePath(env.Region)
	if err := prereq.EnsureCABundle(ctx, config.RDSCABundleURL(env.Region), path); err != nil {
		return "", err
	}
	return path, nil
}

// waitForPort blocks until the local tunnel port accepts connections, the
// forwarder dies, or the timeout elapses.
func waitForPort(t *connector.Tunnel, port int, timeout time.Duration) error {
	ui.Log("Waiting for the tunnel to come up...")
	deadline := time.After(timeout)
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		if prereq.PortOpen(port) {
			return nil
		}
		select {
		case <-t.Done():
			return ui.Errorf("SSM session exited before the tunnel came up (check role permissions, the tunnel host and its SSM agent).")
		case <-deadline:
			return ui.Errorf("timed out waiting for local port %d to open.", port)
		case <-tick.C:
		}
	}
}

// stopProcess terminates the plugin: a polite SIGTERM first (not supported on
// Windows, where it is killed straight away), then a kill after 3 seconds.
func stopProcess(proc *exec.Cmd, done <-chan struct{}) {
	if proc.Process == nil {
		return
	}
	ui.Log("Closing SSM tunnel...")
	if err := proc.Process.Signal(syscall.SIGTERM); err != nil {
		_ = proc.Process.Kill()
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = proc.Process.Kill()
	}
	ui.OK("Tunnel closed.")
}

func roleHint(env *config.Environment, role string) string {
	if env.UsesSSO() {
		return "the " + role + " role"
	}
	return "profile " + env.Profile
}
