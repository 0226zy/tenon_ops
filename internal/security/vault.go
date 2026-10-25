package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
)

const VaultKeyVersion = 1

type Vault struct{ key []byte }

func NewVault(key []byte) *Vault { return &Vault{key: key} }

func (v *Vault) aad(userID string, version int) []byte {
	return []byte(fmt.Sprintf("v1|%s|%d", userID, version))
}

func (v *Vault) Seal(userID string, version int, plaintext string) (nonce, ciphertext []byte, err error) {
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return nonce, gcm.Seal(nil, nonce, []byte(plaintext), v.aad(userID, version)), nil
}

func (v *Vault) Open(userID string, version int, nonce, ciphertext []byte) (string, error) {
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(nonce) != gcm.NonceSize() {
		return "", fmt.Errorf("invalid nonce")
	}
	raw, err := gcm.Open(nil, nonce, ciphertext, v.aad(userID, version))
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(raw), nil
}
