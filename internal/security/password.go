package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

type PasswordParams struct {
	Time    uint32
	Memory  uint32
	Threads uint8
}

var DefaultPasswordParams = PasswordParams{Time: 3, Memory: 64 * 1024, Threads: 2}

func (p PasswordParams) String() string {
	return fmt.Sprintf("m=%d,t=%d,p=%d", p.Memory, p.Time, p.Threads)
}

// HashPassword derives Argon2id(HMAC-SHA256(pepper, password), salt).
func HashPassword(password string, pepper []byte, p PasswordParams) (salt, hash []byte, params string, err error) {
	salt = make([]byte, 16)
	if _, err = rand.Read(salt); err != nil {
		return nil, nil, "", err
	}
	mac := hmac.New(sha256.New, pepper)
	_, _ = mac.Write([]byte(password))
	material := mac.Sum(nil)
	hash = argon2.IDKey(material, salt, p.Time, p.Memory, p.Threads, 32)
	return salt, hash, p.String(), nil
}

func VerifyPassword(password string, pepper, salt, want []byte, params string) bool {
	var memory, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(params, "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false
	}
	if memory < 8 || timeCost < 1 || threads < 1 || len(salt) == 0 || len(want) == 0 {
		return false
	}
	mac := hmac.New(sha256.New, pepper)
	_, _ = mac.Write([]byte(password))
	material := mac.Sum(nil)
	got := argon2.IDKey(material, salt, timeCost, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func RandomPassword() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func RandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func SHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum[:])
}

func TrimPassword(p string) string { return strings.TrimSpace(p) }
