// Package config loads the tenon_ops YAML configuration.
package config

import (
	"bytes"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration struct{ time.Duration }

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be a scalar")
	}
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", n.Value, err)
	}
	d.Duration = v
	return nil
}

type Config struct {
	Mode              string   `yaml:"mode"`        // remote (default) | local
	Exposure          string   `yaml:"exposure"`    // private (default) | public
	Environment       string   `yaml:"environment"` // label in the UI, e.g. tenon_prod
	Listen            string   `yaml:"listen"`
	PublicURL         string   `yaml:"public_url"`
	Timezone          string   `yaml:"timezone"`
	AllowRemoteListen bool     `yaml:"allow_remote_listen"`
	DB                DB       `yaml:"db"`
	Security          Security `yaml:"security"`
	Tenon             Tenon    `yaml:"tenon"`
	UI                UI       `yaml:"ui"`
}

type DB struct {
	DSN         string `yaml:"dsn"`
	DSNEnv      string `yaml:"dsn_env"`
	AdminSchema string `yaml:"admin_schema"`
	TenonSchema string `yaml:"tenon_schema"`
	MaxOpen     int    `yaml:"max_open"`
	MaxIdle     int    `yaml:"max_idle"`
}

type Security struct {
	KeysDir               string   `yaml:"keys_dir"`
	CookieName            string   `yaml:"cookie_name"`
	CookieSecure          *bool    `yaml:"cookie_secure"`
	SessionTTL            Duration `yaml:"session_ttl"`
	IdleTTL               Duration `yaml:"idle_ttl"`
	StepUpTTL             Duration `yaml:"step_up_ttl"`
	LoginFailLimit        int      `yaml:"login_fail_limit"`
	LoginLock             Duration `yaml:"login_lock"`
	TrustedProxies        []string `yaml:"trusted_proxies"`
	AllowedOrigins        []string `yaml:"allowed_origins"`
	AllowedHosts          []string `yaml:"allowed_hosts"`
	RequireStepUpWrites   bool     `yaml:"require_step_up_writes"`
	AllowPublicWithoutMFA bool     `yaml:"allow_public_without_mfa"`
}

type Tenon struct {
	CatalogFile string `yaml:"catalog_file"`
	ControlURL  string `yaml:"control_url"`
}

type UI struct {
	ProductName string `yaml:"product_name"`
	Version     string `yaml:"version"`
}

func Default() *Config {
	return &Config{
		Mode:        "remote",
		Exposure:    "private",
		Environment: "tenon_prod",
		Listen:      "127.0.0.1:7200",
		PublicURL:   "https://admin.example.com",
		Timezone:    "Asia/Shanghai",
		DB: DB{
			DSNEnv:      "TENON_OPS_DSN",
			AdminSchema: "tenon_admin",
			TenonSchema: "tenon_prod",
			MaxOpen:     10,
			MaxIdle:     3,
		},
		Security: Security{
			KeysDir:        "/etc/tenon-ops/keys",
			CookieName:     "",
			SessionTTL:     Duration{8 * time.Hour},
			IdleTTL:        Duration{30 * time.Minute},
			StepUpTTL:      Duration{5 * time.Minute},
			LoginFailLimit: 5,
			LoginLock:      Duration{15 * time.Minute},
			TrustedProxies: []string{"127.0.0.1", "::1"},
		},
		Tenon: Tenon{},
		UI:    UI{ProductName: "Tenon Ops", Version: "0.1.0"},
	}
}

var schemaPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
var cookiePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	c := Default()
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	base := filepath.Dir(abs)
	resolve := func(p *string) {
		if *p != "" && !filepath.IsAbs(*p) {
			*p = filepath.Clean(filepath.Join(base, *p))
		}
	}
	resolve(&c.Security.KeysDir)
	resolve(&c.Tenon.CatalogFile)
	c.Security.KeysDir = expandHome(c.Security.KeysDir)
	if c.DB.DSNEnv == "" {
		c.DB.DSNEnv = "TENON_OPS_DSN"
	}
	if v := os.Getenv(c.DB.DSNEnv); v != "" {
		c.DB.DSN = v
	}
	if c.DB.DSN == "" {
		return nil, fmt.Errorf("db.dsn is empty and %s is not set", c.DB.DSNEnv)
	}
	if c.Mode != "local" && c.Mode != "remote" {
		return nil, fmt.Errorf("mode must be local or remote")
	}
	if c.Exposure != "private" && c.Exposure != "public" {
		return nil, fmt.Errorf("exposure must be private or public")
	}
	if c.Exposure == "public" && !c.Security.AllowPublicWithoutMFA {
		return nil, fmt.Errorf("exposure=public requires MFA; this build has no MFA. Put the service behind VPN/IP allowlist or set security.allow_public_without_mfa=true to accept the risk")
	}
	if !schemaPattern.MatchString(c.DB.AdminSchema) || !schemaPattern.MatchString(c.DB.TenonSchema) {
		return nil, fmt.Errorf("db schema names must match [A-Za-z0-9_]+")
	}
	if c.Listen == "" {
		return nil, fmt.Errorf("listen is required")
	}
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" && !c.AllowRemoteListen {
		return nil, fmt.Errorf("listen must be loopback unless allow_remote_listen=true")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return nil, fmt.Errorf("timezone: %w", err)
	}
	if c.PublicURL == "" {
		return nil, fmt.Errorf("public_url is required (used for Origin and Host allowlists)")
	}
	if c.PublicURL != "" {
		u, err := url.Parse(c.PublicURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("public_url must be an absolute URL")
		}
		if c.Exposure == "public" && u.Scheme != "https" {
			return nil, fmt.Errorf("public_url must use https when exposure=public")
		}
		if len(c.Security.AllowedOrigins) == 0 {
			c.Security.AllowedOrigins = []string{u.Scheme + "://" + u.Host}
		}
	}
	if len(c.Security.AllowedHosts) == 0 && c.PublicURL != "" {
		if u, err := url.Parse(c.PublicURL); err == nil {
			c.Security.AllowedHosts = []string{u.Host}
		}
	}
	if c.Security.CookieName == "" {
		if c.CookieSecure() {
			c.Security.CookieName = "__Host-tenon_ops_sid"
		} else {
			c.Security.CookieName = "tenon_ops_sid"
		}
	}
	if !cookiePattern.MatchString(strings.TrimPrefix(c.Security.CookieName, "__Host-")) {
		return nil, fmt.Errorf("invalid cookie_name")
	}
	if c.Security.SessionTTL.Duration <= 0 {
		c.Security.SessionTTL.Duration = 8 * time.Hour
	}
	if c.Security.IdleTTL.Duration <= 0 {
		c.Security.IdleTTL.Duration = 30 * time.Minute
	}
	if c.Security.StepUpTTL.Duration <= 0 {
		c.Security.StepUpTTL.Duration = 5 * time.Minute
	}
	if c.Security.LoginFailLimit <= 0 {
		c.Security.LoginFailLimit = 5
	}
	if c.Security.LoginLock.Duration <= 0 {
		c.Security.LoginLock.Duration = 15 * time.Minute
	}
	if c.UI.ProductName == "" {
		c.UI.ProductName = "Tenon Ops"
	}
	return c, nil
}

func (c *Config) CookieSecure() bool {
	if c.Security.CookieSecure != nil {
		return *c.Security.CookieSecure
	}
	return c.Exposure == "public" || c.Mode == "remote"
}

func (c *Config) Location() *time.Location {
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
