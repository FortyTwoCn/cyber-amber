package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

type SessionManager struct {
	key []byte
	ttl time.Duration
}
type payload struct {
	Expires int64  `json:"exp"`
	Nonce   string `json:"nonce"`
}

func NewSessionManager(key []byte, ttl time.Duration) (*SessionManager, error) {
	if len(key) < 32 {
		return nil, errors.New("session secret must be at least 32 bytes")
	}
	return &SessionManager{key: key, ttl: ttl}, nil
}
func (s *SessionManager) Create(now time.Time) (token, csrf string, err error) {
	nonce := make([]byte, 24)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", "", err
	}
	body, _ := json.Marshal(payload{Expires: now.Add(s.ttl).Unix(), Nonce: base64.RawURLEncoding.EncodeToString(nonce)})
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := s.mac("session:" + encoded)
	token = encoded + "." + base64.RawURLEncoding.EncodeToString(mac)
	csrf = base64.RawURLEncoding.EncodeToString(s.mac("csrf:" + token))
	return token, csrf, nil
}
func (s *SessionManager) Verify(token string, now time.Time) bool {
	if len(token) < 40 || len(token) > 1024 {
		return false
	}
	encoded, signature, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	provided, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(provided, s.mac("session:"+encoded)) {
		return false
	}
	body, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}
	var value payload
	if json.Unmarshal(body, &value) != nil || value.Nonce == "" || now.Unix() >= value.Expires {
		return false
	}
	return true
}
func (s *SessionManager) VerifyCSRF(token, csrf string) bool {
	if len(token) < 40 || len(token) > 1024 || len(csrf) < 32 || len(csrf) > 256 {
		return false
	}
	provided, err := base64.RawURLEncoding.DecodeString(csrf)
	return err == nil && hmac.Equal(provided, s.mac("csrf:"+token))
}
func (s *SessionManager) CSRF(token string) string {
	return base64.RawURLEncoding.EncodeToString(s.mac("csrf:" + token))
}
func (s *SessionManager) mac(value string) []byte {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}
