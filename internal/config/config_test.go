package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExampleIsValid(t *testing.T) {
	f, err := Parse(Example)
	if err != nil {
		t.Fatalf("embedded example must be valid: %v", err)
	}
	if got := strings.Join(f.EnvironmentNames(), ","); got != "dev,prod,sandbox" {
		t.Fatalf("environments = %s", got)
	}
}

func TestDefaultsApplied(t *testing.T) {
	f, err := Parse([]byte(`
sso: {start_url: https://x.awsapps.com/start}
environments:
  dev:
    region: eu-west-1
    account_id: "123456789012"
    hosts: {jump: {instance_id: i-0123456789abcdef0, role: R}}
    db: {endpoint: db.example.com, name: app, local_port: 15432, via: jump, role: R}
`))
	if err != nil {
		t.Fatal(err)
	}
	if f.SSO.Region != DefaultSSORegion {
		t.Errorf("sso.region default = %q", f.SSO.Region)
	}
	env := f.Environments["dev"]
	if env.Name != "dev" {
		t.Errorf("Name = %q", env.Name)
	}
	db := env.DB
	if db.Port != DefaultDBPort || db.Auth != AuthIAM || db.SSLMode != DefaultSSLMode {
		t.Errorf("db defaults not applied: %+v", db)
	}
	if env.TunnelTarget().InstanceID != "i-0123456789abcdef0" {
		t.Errorf("TunnelTarget = %+v", env.TunnelTarget())
	}
	if !db.NeedsCABundle() {
		t.Error("verify-ca must need a CA bundle")
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	_, err := Parse([]byte(`
environments:
  Bad Name:
    region: us-east-1
    account_id: "123"
    hosts: {jump: {instance_id: not-an-id}}
    db: {endpoint: "", name: app, port: 70000, local_port: 80, via: nowhere, auth: kerberos, ssl_mode: maybe}
`))
	if err == nil {
		t.Fatal("expected validation errors")
	}
	for _, want := range []string{
		"name must be lowercase",
		"account_id: must be a 12-digit",
		"is not an EC2",
		"hosts.jump.role: required",
		"db.endpoint: required",
		"db.port: 70000 out of range",
		"local_port: 80 must be between",
		`via: "nowhere" is not one of`,
		"db.role: required",
		`auth: "kerberos"`,
		`ssl_mode: "maybe"`,
		"sso.start_url: required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in:\n%v", want, err)
		}
	}
}

func TestProfileEnvironmentNeedsNoSSOOrRoles(t *testing.T) {
	f, err := Parse([]byte(`
environments:
  lab:
    region: eu-west-1
    profile: lab
    hosts: {box: {instance_id: mi-0123456789abcdef0}}
`))
	if err != nil {
		t.Fatalf("profile env should be valid without sso/roles: %v", err)
	}
	if f.Environments["lab"].UsesSSO() {
		t.Error("UsesSSO must be false with a profile")
	}
}

func TestDuplicateLocalPort(t *testing.T) {
	_, err := Parse([]byte(`
environments:
  a: {region: r, profile: p, hosts: {h: {instance_id: i-01234567}}, db: {endpoint: e, name: n, local_port: 5555, via: h}}
  b: {region: r, profile: p, hosts: {h: {instance_id: i-01234567}}, db: {endpoint: e, name: n, local_port: 5555, via: h}}
`))
	if err == nil || !strings.Contains(err.Error(), `already used by "a"`) {
		t.Fatalf("expected duplicate port error, got %v", err)
	}
}

func TestUnknownKeyIsAnError(t *testing.T) {
	_, err := Parse([]byte("environments: {}\nsso: {start_url: x, regoin: y}\n"))
	if err == nil || !strings.Contains(err.Error(), "regoin") {
		t.Fatalf("typo'd key must be rejected, got %v", err)
	}
}

func TestEnvironmentSelection(t *testing.T) {
	f, _ := Parse(Example)
	if _, err := f.Environment(""); err == nil || !strings.Contains(err.Error(), "--env is required") {
		t.Errorf("empty env: %v", err)
	}
	if _, err := f.Environment("qa"); err == nil || !strings.Contains(err.Error(), `unknown environment "qa"`) {
		t.Errorf("unknown env: %v", err)
	}
	if env, err := f.Environment("prod"); err != nil || !env.Confirm {
		t.Errorf("prod: %v %+v", err, env)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil || !strings.Contains(err.Error(), "ssmgate config init") {
		t.Errorf("missing file should hint config init: %v", err)
	}
	p := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(p, Example, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err != nil {
		t.Errorf("Load: %v", err)
	}
}

func TestDefaultPathHonoursEnv(t *testing.T) {
	t.Setenv(EnvConfigPath, "/tmp/custom.yaml")
	if got := DefaultPath(); got != "/tmp/custom.yaml" {
		t.Errorf("DefaultPath = %s", got)
	}
}

func TestRDSCABundleURL(t *testing.T) {
	want := "https://truststore.pki.rds.amazonaws.com/eu-west-1/eu-west-1-bundle.pem"
	if got := RDSCABundleURL("eu-west-1"); got != want {
		t.Errorf("got %s", got)
	}
}
