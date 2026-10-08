// Package prereq checks the external tools ssmgate relies on and prepares
// local resources (CA bundle, free local port).
package prereq

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/dapasa/ssmgate/internal/ui"
)

// PluginName is the AWS Session Manager plugin binary. It implements the SSM
// data-channel protocol that the AWS SDK does not include.
const PluginName = "session-manager-plugin"

// PluginInstallURL documents how to install the plugin on every OS.
const PluginInstallURL = "https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html"

// LookPath is exec.LookPath, swappable in tests.
var LookPath = exec.LookPath

// CheckSessionManagerPlugin verifies the session-manager-plugin is on PATH.
func CheckSessionManagerPlugin() error {
	if _, err := LookPath(PluginName); err != nil {
		return ui.Errorf("%s not found on PATH (required for SSM sessions).\n     Install: %s", PluginName, PluginInstallURL)
	}
	return nil
}

// CheckPsql verifies psql is present (only `db` needs it).
func CheckPsql() error {
	if _, err := LookPath("psql"); err != nil {
		return ui.Errorf("psql not found (needed by `db`).\n" +
			"     Install the PostgreSQL client, e.g. sudo apt-get install -y postgresql-client\n" +
			"     Tip: `ssmgate tunnel` does NOT need psql — use it with a GUI client (DBeaver, TablePlus).")
	}
	return nil
}

// EnsureCABundle downloads the CA bundle at url to path if it is missing.
func EnsureCABundle(ctx context.Context, url, path string) error {
	if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
		ui.OK("CA bundle present: %s", path)
		return nil
	}
	ui.Log("Downloading CA bundle from %s ...", url)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ui.Errorf("could not create CA bundle dir: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ui.Errorf("bad CA bundle URL: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ui.Errorf("failed to download CA bundle from %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ui.Errorf("failed to download CA bundle: HTTP %d", resp.StatusCode)
	}
	// Write to a temp file first so an interrupted download never leaves a
	// truncated bundle that would be "present" on the next run.
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return ui.Errorf("could not write CA bundle: %v", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return ui.Errorf("could not save CA bundle: %v", err)
	}
	if err := f.Close(); err != nil {
		return ui.Errorf("could not save CA bundle: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return ui.Errorf("could not save CA bundle: %v", err)
	}
	ui.OK("Saved CA bundle to %s", path)
	return nil
}

// PortOpen reports whether a local TCP port is already accepting connections.
func PortOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// EnsurePortFree fails if something already listens on the local port.
func EnsurePortFree(port int) error {
	if PortOpen(port) {
		return ui.Errorf("local port %d is already in use. Close the other process or change db.local_port.", port)
	}
	return nil
}
