package services

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

// tenonVerify mirrors internal/control/auth/auth.go verify() in Tenon.
func tenonVerify(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var memory, timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func TestTenonPasswordHashCompatibility(t *testing.T) {
	hash, err := tenonPasswordHash("P@ssw0rd-长密码-123456")
	if err != nil {
		t.Fatal(err)
	}
	if len(hash) != 97 {
		t.Fatalf("hash length = %d, want 97", len(hash))
	}
	if !tenonVerify(hash, "P@ssw0rd-长密码-123456") {
		t.Fatal("Tenon verify rejected the generated hash")
	}
	if tenonVerify(hash, "wrong-password") {
		t.Fatal("Tenon verify accepted a wrong password")
	}
}
