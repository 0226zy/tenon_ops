// Package security owns password hashing, key loading and password encryption.
package security

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Keys struct {
	Master  []byte
	Pepper  []byte
	Session []byte
	Vault   []byte
	Audit   []byte
}

// Load reads a 32-byte master.key from dir and derives domain-separated keys.
func Load(dir string) (*Keys, error) {
	path := filepath.Join(dir, "master.key")
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("read master key: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("master.key must not be a symlink")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("master.key permissions must be 0600, got %o", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("master.key must be 32 bytes, got %d", len(raw))
	}
	k := &Keys{Master: raw}
	if k.Pepper, err = derive(raw, "admin-pepper-v1"); err != nil {
		return nil, err
	}
	if k.Session, err = derive(raw, "session-v1"); err != nil {
		return nil, err
	}
	if k.Vault, err = derive(raw, "vault-v1"); err != nil {
		return nil, err
	}
	if k.Audit, err = derive(raw, "audit-v1"); err != nil {
		return nil, err
	}
	return k, nil
}

// Generate writes a new 32-byte master.key with 0600, refusing to overwrite.
func Generate(dir string) ([]byte, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "master.key")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create master.key: %w", err)
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := f.Write(key); err != nil {
		return nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	ok = true
	return key, nil
}

func derive(master []byte, info string) ([]byte, error) {
	return hkdf.Key(sha256.New, master, []byte("tenon-ops"), info, 32)
}
