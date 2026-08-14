package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	value := Redact("request failed: Cookie: SESSDATA=secret; bili_jct=csrf https://x.test/a?upsig=cdn-secret&w_rid=abc&foo=bar")
	if contains(value, "secret") || contains(value, "csrf") || contains(value, "abc") || contains(value, "foo=bar") {
		t.Fatalf("not redacted: %s", value)
	}
}
func contains(s, sub string) bool { return len(sub) > 0 && strings.Contains(s, sub) }

func TestHTTPAndAPIErrorsAreClassified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/risk":
			w.WriteHeader(http.StatusPreconditionFailed)
			_, _ = fmt.Fprint(w, "risk")
		case "/rate":
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = fmt.Fprint(w, "rate")
		case "/drift":
			_, _ = fmt.Fprint(w, `{"unexpected":`)
		}
	}))
	defer server.Close()
	c := New(server.Client(), server.URL, server.URL, "test")
	for _, test := range []struct {
		path            string
		risk, permanent bool
		httpStatus      int
	}{
		{"/risk", true, true, http.StatusPreconditionFailed},
		{"/rate", false, false, http.StatusTooManyRequests},
	} {
		var out map[string]any
		err := c.GetJSON(context.Background(), test.path, nil, &out)
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Risk != test.risk || apiErr.Permanent != test.permanent || apiErr.HTTPStatus != test.httpStatus {
			t.Fatalf("path=%s err=%#v", test.path, err)
		}
	}
	var out map[string]any
	var drift *APIError
	if err := c.GetJSON(context.Background(), "/drift", nil, &out); !errors.As(err, &drift) || !drift.Permanent {
		t.Fatalf("structure drift err=%#v", err)
	}
	for _, code := range []int{-101, -111} {
		var authErr *APIError
		if err := Check(code, "not logged in"); !errors.As(err, &authErr) || !authErr.Permanent {
			t.Fatalf("code=%d err=%#v", code, err)
		}
	}
	var closed *APIError
	if err := Check(12002, "comment area closed"); !errors.As(err, &closed) || !closed.Permanent || closed.Risk {
		t.Fatalf("resource-level failure misclassified: %#v", err)
	}
	var captcha *APIError
	if err := Check(-105, "captcha"); !errors.As(err, &captcha) || !captcha.Permanent || !captcha.Risk {
		t.Fatalf("captcha failure misclassified: %#v", err)
	}
}
