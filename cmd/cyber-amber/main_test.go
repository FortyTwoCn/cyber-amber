package main

import (
	"strings"
	"testing"
)

func TestHealthcheckURLUsesConfiguredListener(t *testing.T) {
	tests := map[string]string{
		":9090":          "http://127.0.0.1:9090/healthz",
		"0.0.0.0:8081":   "http://127.0.0.1:8081/healthz",
		"[::]:8082":      "http://127.0.0.1:8082/healthz",
		"127.0.0.1:8083": "http://127.0.0.1:8083/healthz",
	}
	for input, expected := range tests {
		if actual := healthcheckURL(input); actual != expected {
			t.Errorf("healthcheckURL(%q)=%q, want %q", input, actual, expected)
		}
	}
}

func TestReadPasswordFromStdin(t *testing.T) {
	password, err := readPassword(strings.NewReader("a secure password\r\n"))
	if err != nil || password != "a secure password" {
		t.Fatalf("password=%q err=%v", password, err)
	}
	if _, err := readPassword(strings.NewReader("line one\nline two")); err == nil {
		t.Fatal("embedded newline was accepted")
	}
}
