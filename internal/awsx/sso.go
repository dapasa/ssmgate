package awsx

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	ssooidctypes "github.com/aws/aws-sdk-go-v2/service/ssooidc/types"

	"github.com/dapasa/ssmgate/internal/config"
	"github.com/dapasa/ssmgate/internal/ui"
)

// clientName / clientType used when registering with SSO OIDC.
const (
	oidcClientName  = "ssmgate"
	oidcClientType  = "public"
	deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"
)

// cachedToken is the on-disk SSO access token, in a format compatible with the
// AWS CLI's ~/.aws/sso/cache/<sha1(startUrl)>.json so a token obtained here (or
// by `aws sso login`) can be reused across both tools.
type cachedToken struct {
	StartURL    string    `json:"startUrl"`
	Region      string    `json:"region"`
	AccessToken string    `json:"accessToken"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// cacheDir is the SSO token cache directory, swappable in tests.
var cacheDir = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".aws", "sso", "cache")
}

// Login performs (or reuses) an SSO login against the configured portal and
// returns a valid SSO access token. It uses the OIDC device-authorization
// flow: the verification URL + code are always printed, so it works without a
// local browser (WSL, remote shells); a browser is opened when possible.
func Login(ctx context.Context, s config.SSO) (string, error) {
	if tok, ok := loadCachedToken(s.StartURL); ok {
		ui.OK("Existing SSO session is still valid.")
		return tok, nil
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(s.Region))
	if err != nil {
		return "", ui.Errorf("failed to init AWS config: %v", err)
	}
	oidc := ssooidc.NewFromConfig(cfg)

	reg, err := oidc.RegisterClient(ctx, &ssooidc.RegisterClientInput{
		ClientName: aws.String(oidcClientName),
		ClientType: aws.String(oidcClientType),
		Scopes:     []string{"sso:account:access"},
	})
	if err != nil {
		return "", ui.Errorf("SSO client registration failed: %v", err)
	}

	dev, err := oidc.StartDeviceAuthorization(ctx, &ssooidc.StartDeviceAuthorizationInput{
		ClientId:     reg.ClientId,
		ClientSecret: reg.ClientSecret,
		StartUrl:     aws.String(s.StartURL),
	})
	if err != nil {
		return "", ui.Errorf("SSO device authorization failed: %v", err)
	}

	verifyURL := aws.ToString(dev.VerificationUriComplete)
	if verifyURL == "" {
		verifyURL = aws.ToString(dev.VerificationUri)
	}

	// Always print the URL + code first, so authorization works even with no
	// browser. Then make a best-effort attempt to open it automatically.
	_, _ = fmt.Fprintf(ui.Out, "\n")
	ui.Log("To authorize this session, open the URL below and confirm the code:")
	_, _ = fmt.Fprintf(ui.Out, "\n    URL : %s\n    Code: %s\n\n", aws.ToString(dev.VerificationUri), aws.ToString(dev.UserCode))
	if openBrowser(verifyURL) {
		ui.OK("Opened a browser for you. Approve the request there.")
	} else {
		ui.Warn("Could not auto-open a browser — open the URL above manually (any device).")
		if isWSL() {
			ui.Warn("WSL tip for next time:  sudo apt install -y wslu  (enables auto-open via wslview)")
		}
	}
	ui.Log("Waiting for authorization to complete...")

	token, err := pollForToken(ctx, oidc, reg, dev)
	if err != nil {
		return "", err
	}

	saveCachedToken(cachedToken{
		StartURL:    s.StartURL,
		Region:      s.Region,
		AccessToken: aws.ToString(token.AccessToken),
		ExpiresAt:   time.Now().Add(time.Duration(token.ExpiresIn) * time.Second),
	})
	ui.OK("SSO login complete.")
	return aws.ToString(token.AccessToken), nil
}

// pollForToken polls CreateToken respecting the device interval, until the user
// approves, the code expires, or an unexpected error occurs.
func pollForToken(ctx context.Context, oidc *ssooidc.Client, reg *ssooidc.RegisterClientOutput, dev *ssooidc.StartDeviceAuthorizationOutput) (*ssooidc.CreateTokenOutput, error) {
	interval := time.Duration(dev.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(dev.ExpiresIn) * time.Second)

	for {
		if time.Now().After(deadline) {
			return nil, ui.Errorf("SSO authorization timed out — re-run and approve the request faster.")
		}
		out, err := oidc.CreateToken(ctx, &ssooidc.CreateTokenInput{
			ClientId:     reg.ClientId,
			ClientSecret: reg.ClientSecret,
			DeviceCode:   dev.DeviceCode,
			GrantType:    aws.String(deviceGrantType),
		})
		if err == nil {
			return out, nil
		}

		var pending *ssooidctypes.AuthorizationPendingException
		var slowDown *ssooidctypes.SlowDownException
		switch {
		case errors.As(err, &pending):
			// keep waiting
		case errors.As(err, &slowDown):
			interval += 5 * time.Second
		default:
			return nil, ui.Errorf("SSO token retrieval failed: %v", err)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// RoleCredentials exchanges an SSO access token for short-lived AWS credentials
// for a specific account + role (permission set).
func RoleCredentials(ctx context.Context, s config.SSO, accessToken, accountID, role string) (aws.CredentialsProvider, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(s.Region))
	if err != nil {
		return nil, ui.Errorf("failed to init AWS config: %v", err)
	}
	out, err := sso.NewFromConfig(cfg).GetRoleCredentials(ctx, &sso.GetRoleCredentialsInput{
		AccessToken: aws.String(accessToken),
		AccountId:   aws.String(accountID),
		RoleName:    aws.String(role),
	})
	if err != nil {
		return nil, ui.Errorf("could not assume role %q in account %s: %v\n"+
			"     (Is the %s permission set assigned to you for this account?)", role, accountID, err, role)
	}
	rc := out.RoleCredentials
	ui.OK("Assumed role %s in account %s.", role, accountID)
	return credentials.NewStaticCredentialsProvider(
		aws.ToString(rc.AccessKeyId),
		aws.ToString(rc.SecretAccessKey),
		aws.ToString(rc.SessionToken),
	), nil
}

// --- SSO token cache (AWS CLI-compatible location) ---------------------------

func ssoCachePath(startURL string) string {
	sum := sha1.Sum([]byte(startURL))
	return filepath.Join(cacheDir(), hex.EncodeToString(sum[:])+".json")
}

func loadCachedToken(startURL string) (string, bool) {
	b, err := os.ReadFile(ssoCachePath(startURL))
	if err != nil {
		return "", false
	}
	var t cachedToken
	if err := json.Unmarshal(b, &t); err != nil {
		return "", false
	}
	// Require a comfortable margin so the token does not expire mid-session.
	if t.AccessToken == "" || time.Until(t.ExpiresAt) < 2*time.Minute {
		return "", false
	}
	return t.AccessToken, true
}

func saveCachedToken(t cachedToken) {
	path := ssoCachePath(t.StartURL)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		ui.Warn("could not create SSO cache dir: %v", err)
		return
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		ui.Warn("could not write SSO cache: %v", err)
	}
}
