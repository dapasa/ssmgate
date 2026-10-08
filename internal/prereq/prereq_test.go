package prereq

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dapasa/ssmgate/internal/ui"
)

func init() { ui.Out = io.Discard }

func TestEnsureCABundleDownloadsOnce(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte("-----BEGIN CERTIFICATE-----\n"))
	}))
	defer srv.Close()

	path := filepath.Join(t.TempDir(), "sub", "bundle.pem")
	for i := 0; i < 2; i++ {
		if err := EnsureCABundle(context.Background(), srv.URL, path); err != nil {
			t.Fatal(err)
		}
	}
	if hits != 1 {
		t.Errorf("downloaded %d times, want 1 (cached afterwards)", hits)
	}
	if b, _ := os.ReadFile(path); !strings.HasPrefix(string(b), "-----BEGIN") {
		t.Errorf("bundle content = %q", b)
	}
}

func TestEnsureCABundleHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "bundle.pem")
	if err := EnsureCABundle(context.Background(), srv.URL, path); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("expected HTTP 404 error, got %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("no file must be left behind on failure")
	}
}

func TestPortChecks(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if !PortOpen(port) || EnsurePortFree(port) == nil {
		t.Error("listening port must be reported as in use")
	}
	_ = ln.Close()
	if PortOpen(port) || EnsurePortFree(port) != nil {
		t.Error("closed port must be reported as free")
	}
}

func TestToolChecks(t *testing.T) {
	orig := LookPath
	t.Cleanup(func() { LookPath = orig })

	LookPath = func(string) (string, error) { return "", errors.New("not found") }
	if err := CheckSessionManagerPlugin(); err == nil || !strings.Contains(err.Error(), PluginInstallURL) {
		t.Errorf("missing plugin should point to install docs: %v", err)
	}
	if err := CheckPsql(); err == nil || !strings.Contains(err.Error(), "ssmgate tunnel") {
		t.Errorf("missing psql should suggest tunnel: %v", err)
	}

	LookPath = func(f string) (string, error) { return "/usr/bin/" + f, nil }
	if CheckSessionManagerPlugin() != nil || CheckPsql() != nil {
		t.Error("tools present must pass")
	}
}
