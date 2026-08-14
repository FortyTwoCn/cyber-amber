package auth

import (
	"bytes"
	"testing"
)

func TestEncryptRoundTripAndWrongKey(t *testing.T) {
	key := bytes.Repeat([]byte{1}, 32)
	ciphertext, nonce, err := Encrypt(key, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Decrypt(key, ciphertext, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "secret" {
		t.Fatalf("got %q", plain)
	}
	wrong := bytes.Repeat([]byte{2}, 32)
	if _, err := Decrypt(wrong, ciphertext, nonce); err == nil {
		t.Fatal("wrong key should fail")
	}
}
func TestCookieParsingAndNoSecretExposure(t *testing.T) {
	session, err := ParseCookieHeader("SESSDATA=a; bili_jct=b; DedeUserID=123; evil=x")
	if err != nil {
		t.Fatal(err)
	}
	header := session.CookieHeader()
	if header == "" || session.CSRF() != "b" {
		t.Fatalf("bad session %#v", session)
	}
	if _, ok := session.Cookies["evil"]; ok {
		t.Fatal("unexpected cookie retained")
	}
	if _, err := ParseCookieHeader("SESSDATA=a"); err == nil {
		t.Fatal("incomplete cookie accepted")
	}
	if _, err := ParseCookieHeader("SESSDATA=a%0A; bili_jct=b\nforged; DedeUserID=123"); err == nil {
		t.Fatal("cookie header injection accepted")
	}
	if _, err := ParseCookieHeader("SESSDATA=a; bili_jct=b; DedeUserID=not-a-number"); err == nil {
		t.Fatal("invalid account MID accepted")
	}
}
