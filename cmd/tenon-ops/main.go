package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/0226zy/tenon_ops/internal/app"
	"github.com/0226zy/tenon_ops/internal/config"
	"github.com/0226zy/tenon_ops/internal/security"
	"github.com/0226zy/tenon_ops/internal/store"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch args[0] {
	case "serve":
		err = cmdServe(ctx, args[1:], stdout, stderr)
	case "init-db":
		err = cmdInitDB(ctx, args[1:], stdout, stderr)
	case "init-admin":
		err = cmdInitAdmin(ctx, args[1:], stdout, stderr)
	case "recover-admin":
		err = cmdRecoverAdmin(ctx, args[1:], stdout, stderr)
	case "doctor":
		err = cmdDoctor(ctx, args[1:], stdout, stderr)
	case "backup-keys":
		err = cmdBackupKeys(args[1:], stdout, stderr)
	case "version", "-version", "--version":
		fmt.Fprintln(stdout, "tenon-ops 0.1.0")
	case "help", "-h", "--help":
		usage(stdout)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		usage(stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "tenon-ops:", err)
		return 1
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintln(w, `tenon-ops - Tenon operations console

Usage:
  tenon-ops serve          -config /etc/tenon-ops/config.yaml
  tenon-ops init-db        -config /etc/tenon-ops/config.yaml [-dsn DSN]
  tenon-ops init-admin     -config CONFIG -admin-id ID [--keys-dir DIR] [--secret-dir DIR] [--no-local-secret]
  tenon-ops recover-admin  -config CONFIG -admin-id ID [--secret-dir DIR] [--password PASS]
  tenon-ops doctor         -config CONFIG
  tenon-ops backup-keys    -config CONFIG -out DIR [--secret-dir DIR]
  tenon-ops version

Environment:
  TENON_OPS_CONFIG   default config path (default deploy/config.yaml)
  TENON_OPS_DSN      MySQL DSN for tenon_admin (contains credentials)

Security:
  master.key stays on the server at 0600 (systemd credential recommended).
  admin.secret stays on the administrator's Mac, never on the server.`)
}

func configPath(fs *flag.FlagSet) *string {
	return fs.String("config", envOr("TENON_OPS_CONFIG", "deploy/config.yaml"), "config YAML path")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func loadConfig(path, dsn string) (*config.Config, error) {
	if dsn != "" {
		_ = os.Setenv("TENON_OPS_DSN", dsn)
	}
	return config.Load(path)
}

func cmdInitDB(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("init-db", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := configPath(fs)
	dsn := fs.String("dsn", "", "admin DSN; overrides TENON_OPS_DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*cfgPath, *dsn)
	if err != nil {
		return err
	}
	if err := store.InitSchema(ctx, cfg.DB.DSN, cfg.DB.AdminSchema); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "schema initialised in %s\n", cfg.DB.AdminSchema)
	return nil
}

func cmdServe(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := configPath(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*cfgPath, "")
	if err != nil {
		return err
	}
	a, err := app.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer a.Close()
	count, err := a.Store.AdminCount(ctx)
	if err != nil {
		return fmt.Errorf("admin schema is not initialized (run tenon-ops init-db): %w", err)
	}
	if count == 0 {
		return errors.New("no administrator configured; run tenon-ops init-admin")
	}
	if cfg.Security.KeysDir == "" {
		return errors.New("security.keys_dir is required")
	}
	return a.Run(ctx)
}

func cmdInitAdmin(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("init-admin", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := configPath(fs)
	adminID := fs.String("admin-id", "", "administrator id")
	displayName := fs.String("display-name", "", "display name")
	password := fs.String("password", "", "password (default: generated)")
	keysDir := fs.String("keys-dir", "", "override security.keys_dir; use ./bootstrap/keys for remote")
	secretDir := fs.String("secret-dir", "", "directory for admin.secret on the Mac (default ~/.tenon-ops)")
	noLocalSecret := fs.Bool("no-local-secret", false, "do not write admin.secret (server fallback)")
	reuseKey := fs.Bool("reuse-key", false, "reuse an existing master.key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *adminID == "" {
		return errors.New("-admin-id is required")
	}
	cfg, err := loadConfig(*cfgPath, "")
	if err != nil {
		return err
	}
	if *keysDir != "" {
		cfg.Security.KeysDir = expand(*keysDir)
	}
	if *displayName == "" {
		*displayName = *adminID
	}
	if *password == "" {
		p, err := security.RandomPassword()
		if err != nil {
			return err
		}
		*password = p
	}
	if len(*password) < 12 || len(*password) > 128 {
		return errors.New("admin password must be 12-128 bytes")
	}
	if *reuseKey {
		if _, err := security.Load(cfg.Security.KeysDir); err != nil {
			return err
		}
	} else {
		if _, err := os.Stat(filepath.Join(cfg.Security.KeysDir, "master.key")); err == nil {
			return fmt.Errorf("master.key already exists in %s; use -reuse-key to reuse or remove it deliberately", cfg.Security.KeysDir)
		}
		if _, err := security.Generate(cfg.Security.KeysDir); err != nil {
			return err
		}
	}
	keys, err := security.Load(cfg.Security.KeysDir)
	if err != nil {
		return err
	}
	db, err := store.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.CheckTenonSchema(ctx); err != nil {
		return err
	}
	count, err := db.AdminCount(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return errors.New("an administrator already exists; use recover-admin")
	}
	salt, hash, params, err := security.HashPassword(*password, keys.Pepper, security.DefaultPasswordParams)
	if err != nil {
		return err
	}
	a := &store.Admin{
		AdminID: *adminID, DisplayName: *displayName, Role: "super_admin",
		PwdAlgo: "argon2id-pepper-v1", PwdParams: params, PwdSalt: salt, PwdHash: hash,
		PwdVersion: 1, SessionEpoch: 1, Status: 1, PwdChangedAt: time.Now().UTC(),
	}
	if err := db.CreateAdmin(ctx, a); err != nil {
		return err
	}
	if !*noLocalSecret {
		dir := *secretDir
		if dir == "" {
			dir = defaultSecretDir()
		}
		path, err := writeAdminSecret(dir, *adminID, *password, cfg.Mode)
		if err != nil {
			return fmt.Errorf("admin created but failed to write local secret: %w", err)
		}
		fmt.Fprintf(stdout, "admin.secret: %s (0600, keep it on this Mac)\n", path)
	}
	fmt.Fprintf(stdout, "administrator created: %s\n", *adminID)
	fmt.Fprintf(stdout, "master.key: %s (0600; deploy to the server)\n", filepath.Join(cfg.Security.KeysDir, "master.key"))
	fmt.Fprintf(stdout, "\nPASSWORD (shown once): %s\n\n", *password)
	fmt.Fprintln(stdout, "Remote deployment:")
	fmt.Fprintln(stdout, "  1) copy master.key to the server /etc/tenon-ops/keys/master.key (0600, tenon-ops user)")
	fmt.Fprintln(stdout, "  2) configure systemd + nginx, then start tenon-ops")
	fmt.Fprintln(stdout, "  3) delete any local bootstrap copy of master.key after deployment")
	return nil
}

func cmdRecoverAdmin(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("recover-admin", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := configPath(fs)
	adminID := fs.String("admin-id", "", "administrator id")
	password := fs.String("password", "", "new password (default: generated)")
	secretDir := fs.String("secret-dir", "", "directory for admin.secret (default ~/.tenon-ops)")
	noLocalSecret := fs.Bool("no-local-secret", false, "do not write admin.secret")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *adminID == "" {
		return errors.New("-admin-id is required")
	}
	cfg, err := loadConfig(*cfgPath, "")
	if err != nil {
		return err
	}
	keys, err := security.Load(cfg.Security.KeysDir)
	if err != nil {
		return err
	}
	db, err := store.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.AdminByID(ctx, *adminID); err != nil {
		return err
	}
	if *password == "" {
		p, err := security.RandomPassword()
		if err != nil {
			return err
		}
		*password = p
	}
	salt, hash, params, err := security.HashPassword(*password, keys.Pepper, security.DefaultPasswordParams)
	if err != nil {
		return err
	}
	if err := db.UpdateAdminVerifier(ctx, *adminID, salt, hash, params, "argon2id-pepper-v1"); err != nil {
		return err
	}
	_ = db.RevokeAdminSessions(ctx, *adminID)
	if !*noLocalSecret {
		dir := *secretDir
		if dir == "" {
			dir = defaultSecretDir()
		}
		path, err := writeAdminSecret(dir, *adminID, *password, cfg.Mode)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "admin.secret updated: %s\n", path)
	}
	fmt.Fprintf(stdout, "\nPASSWORD (shown once): %s\n", *password)
	return nil
}

func cmdDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := configPath(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*cfgPath, "")
	if err != nil {
		return err
	}
	a, err := app.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer a.Close()
	h, err := a.Services.System.Health(ctx)
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(h, "", "  ")
	fmt.Fprintln(stdout, string(b))
	if !h.OK {
		return errors.New("doctor found errors")
	}
	return nil
}

func cmdBackupKeys(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("backup-keys", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfgPath := configPath(fs)
	out := fs.String("out", "", "output directory")
	secretDir := fs.String("secret-dir", "", "directory containing admin.secret")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-out is required")
	}
	cfg, err := loadConfig(*cfgPath, "")
	if err != nil {
		return err
	}
	if _, err := security.Load(cfg.Security.KeysDir); err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(cfg.Security.KeysDir, "master.key"), filepath.Join(*out, "master.key")); err != nil {
		return err
	}
	dir := *secretDir
	if dir == "" {
		dir = defaultSecretDir()
	}
	if _, err := os.Stat(filepath.Join(dir, "admin.secret")); err == nil {
		if err := copyFile(filepath.Join(dir, "admin.secret"), filepath.Join(*out, "admin.secret")); err != nil {
			return err
		}
	}
	note := "tenon-ops key backup\n\nThis directory contains master.key (server secret) and optionally admin.secret (Mac secret).\nEncrypt it with age/openssl and store it offline, separate from MySQL backups.\n"
	if err := os.WriteFile(filepath.Join(*out, "README.txt"), []byte(note), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "backup written to %s\n", *out)
	return nil
}

func defaultSecretDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".tenon-ops"
	}
	return filepath.Join(home, ".tenon-ops")
}

func writeAdminSecret(dir, adminID, password, mode string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	body := fmt.Sprintf("ADMIN_ID=%s\nPASSWORD=%s\nCREATED_AT=%s\nMODE=%s\n", adminID, password, time.Now().UTC().Format(time.RFC3339), mode)
	path := filepath.Join(dir, "admin.secret")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, nil
}

func copyFile(src, dst string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0o600)
}

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
