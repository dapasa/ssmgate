package awsx

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/dapasa/ssmgate/internal/config"
)

func TestUserFromARN(t *testing.T) {
	cases := map[string]string{
		"arn:aws:sts::123456789012:assumed-role/AWSReservedSSO_RDSOperator_abc/alice@example.com": "alice",
		"arn:aws:sts::123456789012:assumed-role/Admin/bob":                                        "bob",
		"arn:aws:iam::123456789012:user/carol":                                                    "carol",
		"arn:aws:sts::123456789012:assumed-role/R/":                                               "",
	}
	for arn, want := range cases {
		if got := UserFromARN(arn); got != want {
			t.Errorf("UserFromARN(%q) = %q, want %q", arn, got, want)
		}
	}
}

func TestPluginArgsShell(t *testing.T) {
	out := &ssm.StartSessionOutput{SessionId: aws.String("s-1"), TokenValue: aws.String("tok"), StreamUrl: aws.String("wss://x")}
	args, err := pluginArgs("eu-west-1", "i-0123456789abcdef0", out, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 6 || args[1] != "eu-west-1" || args[2] != "StartSession" || args[3] != "" {
		t.Fatalf("unexpected args: %q", args)
	}
	if args[5] != "https://ssm.eu-west-1.amazonaws.com" {
		t.Errorf("endpoint = %s", args[5])
	}
	var resp map[string]string
	if err := json.Unmarshal([]byte(args[0]), &resp); err != nil || resp["TokenValue"] != "tok" {
		t.Errorf("response json = %s (%v)", args[0], err)
	}
	if strings.Contains(args[4], "DocumentName") {
		t.Errorf("shell params must not carry a document: %s", args[4])
	}
}

func TestPluginArgsPortForward(t *testing.T) {
	params := map[string][]string{"portNumber": {"5432"}, "localPortNumber": {"55432"}, "host": {"db"}}
	args, err := pluginArgs("us-east-1", "i-0123456789abcdef0", &ssm.StartSessionOutput{}, params)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Target       string
		DocumentName string
		Parameters   map[string][]string
	}
	if err := json.Unmarshal([]byte(args[4]), &p); err != nil {
		t.Fatal(err)
	}
	if p.DocumentName != config.SSMPortForwardDocument || p.Parameters["localPortNumber"][0] != "55432" || p.Target != "i-0123456789abcdef0" {
		t.Errorf("params = %+v", p)
	}
}

func TestSSOTokenCache(t *testing.T) {
	dir := t.TempDir()
	orig := cacheDir
	cacheDir = func() string { return dir }
	t.Cleanup(func() { cacheDir = orig })

	const url = "https://example.awsapps.com/start"
	if _, ok := loadCachedToken(url); ok {
		t.Fatal("empty cache must miss")
	}

	saveCachedToken(cachedToken{StartURL: url, AccessToken: "valid", ExpiresAt: time.Now().Add(time.Hour)})
	if tok, ok := loadCachedToken(url); !ok || tok != "valid" {
		t.Fatalf("got %q %v", tok, ok)
	}
	if _, ok := loadCachedToken("https://other.awsapps.com/start"); ok {
		t.Error("cache is keyed by start URL")
	}

	// A token about to expire is not reused.
	saveCachedToken(cachedToken{StartURL: url, AccessToken: "old", ExpiresAt: time.Now().Add(time.Minute)})
	if _, ok := loadCachedToken(url); ok {
		t.Error("token expiring within 2 minutes must not be reused")
	}

	// Corrupt cache file is ignored.
	if err := os.WriteFile(ssoCachePath(url), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := loadCachedToken(url); ok {
		t.Error("corrupt cache must miss")
	}
}

func TestSSOCachePathIsCLICompatible(t *testing.T) {
	// The AWS CLI names the file sha1(startUrl).json.
	got := ssoCachePath("https://my-sso-portal.awsapps.com/start")
	if !strings.HasSuffix(got, "c7aaaf71fcc8777ae2475525ed049d39fe16c484.json") {
		t.Errorf("unexpected cache path %s", got)
	}
}
