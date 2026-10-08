package awsx

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/dapasa/ssmgate/internal/config"
	"github.com/dapasa/ssmgate/internal/ui"
)

// EnvDBUser overrides the database user for a single run.
const EnvDBUser = "SSMGATE_DB_USER"

// UserFromARN derives a database user from an STS caller ARN: the last path
// segment (SSO session name or IAM user name), without any "@domain".
//
// With SSO the session name is usually the corporate e-mail, while the
// Postgres role is the local part only — an rds-db:connect policy scoped to
// the full e-mail would deny access.
func UserFromARN(arn string) string {
	session := arn[strings.LastIndex(arn, "/")+1:]
	if i := strings.Index(session, "@"); i >= 0 {
		session = session[:i]
	}
	return session
}

// resolveDBUser picks the database user: SSMGATE_DB_USER > db.user in the
// config > derived from the caller identity.
func resolveDBUser(ctx context.Context, cfg aws.Config, db *config.Database) (string, error) {
	if v := os.Getenv(EnvDBUser); v != "" {
		ui.OK("DB user (from %s): %s", EnvDBUser, v)
		return v, nil
	}
	if db.User != "" {
		ui.OK("DB user (from config): %s", db.User)
		return db.User, nil
	}
	out, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", ui.Errorf("could not read caller identity: %v", err)
	}
	user := UserFromARN(aws.ToString(out.Arn))
	if user == "" {
		return "", ui.Errorf("could not derive a DB user; set db.user in the config or %s", EnvDBUser)
	}
	ui.OK("DB user (from your AWS identity): %s", user)
	return user, nil
}

// buildAuthToken generates a short-lived (~15 min) RDS IAM auth token.
//
// The token is signed for the REAL database endpoint even though the client
// connects through localhost via the SSM tunnel.
func buildAuthToken(ctx context.Context, cfg aws.Config, db *config.Database, dbUser string) (string, error) {
	ui.Log("Generating 15-min RDS IAM auth token for user %q...", dbUser)
	token, err := auth.BuildAuthToken(ctx, fmt.Sprintf("%s:%d", db.Endpoint, db.Port), cfg.Region, dbUser, cfg.Credentials)
	if err != nil {
		return "", ui.Errorf("failed to generate DB auth token: %v", err)
	}
	ui.OK("Token generated (valid ~15 minutes).")
	return token, nil
}
