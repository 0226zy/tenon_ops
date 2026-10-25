package security

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPasswordRoundTrip(t *testing.T) {
	pepper := bytes.Repeat([]byte{7}, 32)
	password := "correct horse battery staple"
	salt, hash, params, err := HashPassword(password, pepper, DefaultPasswordParams)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(password, pepper, salt, hash, params) {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword("wrong", pepper, salt, hash, params) {
		t.Fatal("wrong password accepted")
	}
	// A different pepper must not verify, even with the same salt/hash params.
	other := bytes.Repeat([]byte{8}, 32)
	if VerifyPassword(password, other, salt, hash, params) {
		t.Fatal("password verified with a different pepper")
	}
}

func TestVaultAAD(t *testing.T) {
	v := NewVault(bytes.Repeat([]byte{1}, 32))
	nonce, ciphertext, err := v.Seal("alice", 1, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Open("alice", 1, nonce, ciphertext)
	if err != nil || got != "s3cret" {
		t.Fatalf("open: %q %v", got, err)
	}
	if _, err := v.Open("bob", 1, nonce, ciphertext); err == nil {
		t.Fatal("ciphertext accepted for another user")
	}
	if _, err := v.Open("alice", 2, nonce, ciphertext); err == nil {
		t.Fatal("ciphertext accepted for another version")
	}
}

func TestSessionAndCSRF(t *testing.T) {
	key := bytes.Repeat([]byte{2}, 32)
	sid, err := NewSessionID()
	if err != nil {
		t.Fatal(err)
	}
	token := SessionToken{AdminID: "admin", SID: sid, IssuedAt: time.Now().Unix(), ExpireAt: time.Now().Add(time.Hour).Unix(), Epoch: 1}
	raw, err := SignSession(key, token)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSession(key, raw)
	if err != nil || parsed.AdminID != "admin" || parsed.SID != sid {
		t.Fatalf("parse: %+v %v", parsed, err)
	}
	if _, err := ParseSession(bytes.Repeat([]byte{3}, 32), raw); err == nil {
		t.Fatal("session accepted with another key")
	}
	expired := token
	expired.ExpireAt = time.Now().Add(-time.Minute).Unix()
	rawExpired, _ := SignSession(key, expired)
	if _, err := ParseSession(key, rawExpired); err == nil {
		t.Fatal("expired session accepted")
	}
	csrf := CSRFToken(key, sid)
	if !VerifyCSRF(key, sid, csrf) || VerifyCSRF(key, sid, "nope") {
		t.Fatal("csrf verification incorrect")
	}
}

func TestKeyGenerationAndPermissions(t *testing.T) {
	dir := t.TempDir()
	if _, err := Generate(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o", info.Mode().Perm())
	}
	keys, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys.Pepper) != 32 || len(keys.Vault) != 32 || len(keys.Session) != 32 {
		t.Fatal("derived keys have wrong length")
	}
	if err := os.Chmod(filepath.Join(dir, "master.key"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("world-readable key accepted")
	}
}
