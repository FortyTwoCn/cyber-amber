package security

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestArgonPassword(t *testing.T) {
	encoded, err := HashPassword("a-long-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := VerifyPassword(encoded, "a-long-admin-password")
	if err != nil || !ok {
		t.Fatalf("verify %v %v", ok, err)
	}
	ok, _ = VerifyPassword(encoded, "wrong-password")
	if ok {
		t.Fatal("wrong password accepted")
	}
}
func TestSessionAndCSRF(t *testing.T) {
	manager, err := NewSessionManager(bytes.Repeat([]byte{1}, 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	token, csrf, err := manager.Create(now)
	if err != nil {
		t.Fatal(err)
	}
	if !manager.Verify(token, now) || !manager.VerifyCSRF(token, csrf) {
		t.Fatal("valid session rejected")
	}
	if manager.Verify(token, now.Add(time.Hour)) || manager.Verify(token+strings.Repeat("x", 1024), now) || manager.VerifyCSRF(token, csrf+"x") {
		t.Fatal("invalid session accepted")
	}
}
