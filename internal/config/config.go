// Package config loads and validates the ssmgate configuration file.
//
// ssmgate is infrastructure-agnostic: every account, instance, role and
// database it can reach is declared by the user in a YAML file (see
// example.yaml). Nothing about a particular organisation is compiled into the
// binary.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Example is the annotated sample configuration. It is written by
// `ssmgate config init` and is also the configuration used in --demo mode.
//
//go:embed example.yaml
var Example []byte

// EnvConfigPath is the environment variable that overrides the config path.
const EnvConfigPath = "SSMGATE_CONFIG"

// Defaults applied to optional fields.
const (
	DefaultDBPort    = 5432
	DefaultSSLMode   = "verify-ca"
	DefaultSSORegion = "us-east-1"
)

// DB authentication modes.
const (
	AuthIAM      = "iam"      // short-lived RDS IAM token used as the password
	AuthPassword = "password" // the client prompts for a regular password
)

// SSMPortForwardDocument is the AWS-managed SSM document used to forward a
// local port to a remote host through a target instance.
const SSMPortForwardDocument = "AWS-StartPortForwardingSessionToRemoteHost"

// File is the root of the configuration file.
type File struct {
	SSO          SSO                     `yaml:"sso"`
	Environments map[string]*Environment `yaml:"environments"`
}

// SSO describes the IAM Identity Center portal used for login. It is only
// required when at least one environment authenticates through SSO.
type SSO struct {
	StartURL string `yaml:"start_url"`
	Region   string `yaml:"region"`
}

// Environment is one target environment (dev, staging, prod, ...).
type Environment struct {
	Name string `yaml:"-"`

	// Region is the AWS region of the instances and the database.
	Region string `yaml:"region"`

	// AccountID + SSO roles are used when Profile is empty.
	AccountID string `yaml:"account_id"`

	// Profile, when set, uses an existing AWS shared-config profile
	// (~/.aws/config) instead of the built-in SSO login. Roles are then
	// ignored: the profile's own credentials are used for every action.
	Profile string `yaml:"profile"`

	// Confirm requires the user to type the environment name before any
	// action (recommended for production). Bypass with --yes.
	Confirm bool `yaml:"confirm"`

	Hosts map[string]Host `yaml:"hosts"`
	DB    *Database       `yaml:"db"`
}

// Host is a `shell` target: an SSM-managed instance and the SSO role
// (permission set) needed to open a session on it.
type Host struct {
	InstanceID string `yaml:"instance_id"`
	Role       string `yaml:"role"`
}

// Database describes a database reachable through an SSM port-forward.
type Database struct {
	Endpoint  string `yaml:"endpoint"`
	Port      int    `yaml:"port"`
	Name      string `yaml:"name"`
	LocalPort int    `yaml:"local_port"`
	// Via is the key of the host (in Hosts) the tunnel goes through.
	Via string `yaml:"via"`
	// Role is the SSO role used for the tunnel and the IAM token.
	Role string `yaml:"role"`
	// Auth is "iam" (default) or "password".
	Auth string `yaml:"auth"`
	// User is the database user. Empty means: derive it from the caller's
	// AWS identity (the part of the session name before "@").
	User string `yaml:"user"`
	// SSLMode is passed to psql. Default verify-ca (see README for why not
	// verify-full through a tunnel).
	SSLMode string `yaml:"ssl_mode"`
	// CABundle is the root certificate path. Empty with a verify-* SSL mode
	// means: download the AWS RDS regional bundle automatically.
	CABundle string `yaml:"ca_bundle"`
}

// UsesSSO reports whether the environment authenticates through SSO.
func (e *Environment) UsesSSO() bool { return e.Profile == "" }

// HostNames returns the environment's host keys, sorted.
func (e *Environment) HostNames() []string {
	names := make([]string, 0, len(e.Hosts))
	for k := range e.Hosts {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// TunnelTarget returns the instance the database tunnel goes through.
func (e *Environment) TunnelTarget() Host {
	return e.Hosts[e.DB.Via]
}

// EnvironmentNames returns the configured environment names, sorted.
func (f *File) EnvironmentNames() []string {
	names := make([]string, 0, len(f.Environments))
	for k := range f.Environments {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Environment returns a validated environment by name. An empty name is an
// error: ssmgate never picks an environment for you.
func (f *File) Environment(name string) (*Environment, error) {
	if name == "" {
		return nil, fmt.Errorf("--env is required (one of: %s). The environment is never defaulted", strings.Join(f.EnvironmentNames(), ", "))
	}
	env, ok := f.Environments[name]
	if !ok {
		return nil, fmt.Errorf("unknown environment %q (configured: %s)", name, strings.Join(f.EnvironmentNames(), ", "))
	}
	return env, nil
}

// DefaultPath returns the default config location:
// <user config dir>/ssmgate/config.yaml, overridable with SSMGATE_CONFIG.
func DefaultPath() string {
	if p := os.Getenv(EnvConfigPath); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "ssmgate", "config.yaml")
}

// Load reads, parses and validates the config file at path.
func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("config file not found at %s\n     Create one with: ssmgate config init   (or try everything offline with --demo)", path)
		}
		return nil, fmt.Errorf("could not read config %s: %w", path, err)
	}
	f, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

// Parse parses and validates config bytes, applying defaults.
func Parse(b []byte) (*File, error) {
	var f File
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true) // typos in keys are errors, not silently ignored
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	f.applyDefaults()
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

func (f *File) applyDefaults() {
	if f.SSO.Region == "" {
		f.SSO.Region = DefaultSSORegion
	}
	for name, env := range f.Environments {
		if env == nil {
			continue
		}
		env.Name = name
		if env.DB != nil {
			if env.DB.Port == 0 {
				env.DB.Port = DefaultDBPort
			}
			if env.DB.Auth == "" {
				env.DB.Auth = AuthIAM
			}
			if env.DB.SSLMode == "" {
				env.DB.SSLMode = DefaultSSLMode
			}
		}
	}
}

var (
	envNameRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	accountIDRe  = regexp.MustCompile(`^\d{12}$`)
	instanceIDRe = regexp.MustCompile(`^(i|mi)-[0-9a-f]{8,17}$`)
)

// Validate checks the whole file and returns every problem found at once,
// so a user can fix their config in a single pass.
func (f *File) Validate() error {
	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if len(f.Environments) == 0 {
		add("no environments defined")
	}
	needsSSO := false
	for _, name := range f.EnvironmentNames() {
		env := f.Environments[name]
		p := "environments." + name
		if env == nil {
			add("%s: is empty", p)
			continue
		}
		if !envNameRe.MatchString(name) {
			add("%s: name must be lowercase letters, digits, '-' or '_'", p)
		}
		if env.Region == "" {
			add("%s.region: required", p)
		}
		if env.UsesSSO() {
			needsSSO = true
			if !accountIDRe.MatchString(env.AccountID) {
				add("%s.account_id: must be a 12-digit AWS account id (or set profile)", p)
			}
		}
		if len(env.Hosts) == 0 {
			add("%s.hosts: at least one host is required", p)
		}
		for _, hn := range env.HostNames() {
			h := env.Hosts[hn]
			hp := p + ".hosts." + hn
			if !instanceIDRe.MatchString(h.InstanceID) {
				add("%s.instance_id: %q is not an EC2 (i-...) or managed (mi-...) instance id", hp, h.InstanceID)
			}
			if env.UsesSSO() && h.Role == "" {
				add("%s.role: required when the environment uses SSO", hp)
			}
		}
		if db := env.DB; db != nil {
			dp := p + ".db"
			if db.Endpoint == "" {
				add("%s.endpoint: required", dp)
			}
			if db.Name == "" {
				add("%s.name: required", dp)
			}
			if db.Port < 1 || db.Port > 65535 {
				add("%s.port: %d out of range", dp, db.Port)
			}
			if db.LocalPort < 1024 || db.LocalPort > 65535 {
				add("%s.local_port: %d must be between 1024 and 65535", dp, db.LocalPort)
			}
			if _, ok := env.Hosts[db.Via]; !ok {
				add("%s.via: %q is not one of this environment's hosts (%s)", dp, db.Via, strings.Join(env.HostNames(), ", "))
			}
			if env.UsesSSO() && db.Role == "" {
				add("%s.role: required when the environment uses SSO", dp)
			}
			if db.Auth != AuthIAM && db.Auth != AuthPassword {
				add("%s.auth: %q must be %q or %q", dp, db.Auth, AuthIAM, AuthPassword)
			}
			switch db.SSLMode {
			case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
			default:
				add("%s.ssl_mode: %q is not a valid libpq sslmode", dp, db.SSLMode)
			}
		}
	}
	// Two environments on the same local port could never run side by side.
	seen := map[int]string{}
	for _, name := range f.EnvironmentNames() {
		env := f.Environments[name]
		if env == nil || env.DB == nil {
			continue
		}
		if other, dup := seen[env.DB.LocalPort]; dup {
			add("environments.%s.db.local_port: %d is already used by %q", name, env.DB.LocalPort, other)
		}
		seen[env.DB.LocalPort] = name
	}
	if needsSSO && !strings.HasPrefix(f.SSO.StartURL, "https://") {
		add("sso.start_url: required (https://...) because at least one environment uses SSO")
	}
	return errors.Join(errs...)
}

// NeedsCABundle reports whether psql must be given a root certificate.
func (d *Database) NeedsCABundle() bool {
	return strings.HasPrefix(d.SSLMode, "verify-")
}

// RDSCABundleURL / RDSCABundlePath return the AWS RDS regional CA bundle
// location, downloaded on first use when no ca_bundle is configured.
func RDSCABundleURL(region string) string {
	return fmt.Sprintf("https://truststore.pki.rds.amazonaws.com/%s/%s-bundle.pem", region, region)
}

func RDSCABundlePath(region string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".aws", fmt.Sprintf("rds-%s-bundle.pem", region))
}
