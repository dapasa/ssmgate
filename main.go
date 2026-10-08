// Command ssmgate gives secure, auditable access to private AWS hosts and
// databases entirely over AWS Systems Manager (SSM) Session Manager: no SSH,
// no inbound ports, no bastion keys. Credentials come from IAM Identity Center
// (SSO) or an AWS profile, and every target is declared in a YAML config.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/dapasa/ssmgate/cmd"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() { os.Exit(run()) }

func run() int {
	// Cancel the root context on SIGINT/SIGTERM so tunnels/sessions tear down
	// cleanly. Interactive children (psql, the plugin shell) receive the same
	// signal directly from the terminal.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cmd.NewRootCmd(version).ExecuteContext(ctx); err != nil {
		if !errors.Is(err, context.Canceled) {
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
		return 1
	}
	return 0
}
