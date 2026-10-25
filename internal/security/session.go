package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type SessionToken struct {
	Version  int    `json:"v"`
	AdminID  string `json:"a"`
	SID      string `json:"s"`
	IssuedAt int64  `json:"i"`
	ExpireAt int64  `json:"e"`
	Epoch    int64  `json:"x"`
}

func NewSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func SignSession(key []byte, t SessionToken) (string, error) {
	t.Version = 1
	raw, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig, nil
}

func ParseSession(key []byte, raw string) (SessionToken, error) {
	var t SessionToken
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return t, errors.New("invalid session token")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(parts[0]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(want)) != 1 {
		return t, errors.New("invalid session signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return t, err
	}
	if err := json.Unmarshal(payload, &t); err != nil {
		return t, err
	}
	if t.Version != 1 || t.AdminID == "" || t.SID == "" {
		return t, errors.New("invalid session payload")
	}
	if time.Now().Unix() >= t.ExpireAt {
		return t, errors.New("session expired")
	}
	return t, nil
}

func CSRFToken(key []byte, sid string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("csrf:" + sid))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func VerifyCSRF(key []byte, sid, token string) bool {
	want := CSRFToken(key, sid)
	return subtle.ConstantTimeCompare([]byte(want), []byte(token)) == 1
}
