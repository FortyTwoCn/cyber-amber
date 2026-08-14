package wbi

import (
	"net/url"
	"testing"
)

func TestWBIKnownVector(t *testing.T) {
	values := url.Values{"foo": {"114"}, "bar": {"514"}, "zab": {"1919810"}}
	got, err := Sign(values, "7cd084941338484aae1ad9425b84077c", "4932caff0ff746eab6f01bf08b70ac45", 1702204169)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("w_rid") != "8f6f2b5b3d485fe1886cec6a0be8c5d4" {
		t.Fatalf("got %s", got.Get("w_rid"))
	}
	if got.Get("wts") != "1702204169" {
		t.Fatal("missing wts")
	}
}

func TestWBIFiltersForbiddenCharacters(t *testing.T) {
	got, err := Sign(url.Values{"x": {"a!b'c(d)e*f"}}, "7cd084941338484aae1ad9425b84077c", "4932caff0ff746eab6f01bf08b70ac45", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("x") != "abcdef" {
		t.Fatalf("got %q", got.Get("x"))
	}
}

func TestWBIReplacesStaleSignatureBeforeHashing(t *testing.T) {
	keys := []string{"7cd084941338484aae1ad9425b84077c", "4932caff0ff746eab6f01bf08b70ac45"}
	clean, err := Sign(url.Values{"foo": {"114"}}, keys[0], keys[1], 1702204169)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := Sign(url.Values{"foo": {"114"}, "w_rid": {"attacker-controlled-stale-value"}}, keys[0], keys[1], 1702204169)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Get("w_rid") != clean.Get("w_rid") {
		t.Fatal("stale w_rid contaminated the new signature")
	}
}
