package jev

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNoulsRequestAndResponse(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"a":{"type":"noul","noul":0.71},"b":{"type":"noul","noul":0.03}},"usage":{"input_tokens":10,"output_tokens":2}}`))
	}))
	defer srv.Close()

	c := &Client{Key: "k", URL: srv.URL}
	probs, model, err := c.Nouls(context.Background(), map[string]any{"goal": "g"}, map[string]Noul{
		"a": {Instructions: "Is A needed?", True: "yes", False: "no"},
		"b": {Instructions: "Is B needed?"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if probs["a"] != 0.71 || probs["b"] != 0.03 || model != "jev-1.13.0" {
		t.Errorf("got %v %q", probs, model)
	}
	if got["model"] != DefaultModel {
		t.Errorf("model = %v", got["model"])
	}
	qs := got["questions"].(map[string]any)
	a := qs["a"].(map[string]any)
	if a["type"] != "noul" || a["criteria"].(map[string]any)["true"] != "yes" {
		t.Errorf("question a = %v", a)
	}
	if _, has := qs["b"].(map[string]any)["criteria"]; has {
		t.Error("criteria must be omitted when none is given")
	}
}

// Every failure is an error the caller turns into "no opinion".
func TestNoulsErrors(t *testing.T) {
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"detail":{"error_type":"authentication_error"}}`))
	}))
	defer forbidden.Close()
	if _, _, err := (&Client{Key: "bad", URL: forbidden.URL}).Nouls(context.Background(), "s", map[string]Noul{"q": {Instructions: "?"}}); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("403: err = %v", err)
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	defer slow.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := (&Client{Key: "k", URL: slow.URL}).Nouls(ctx, "s", map[string]Noul{"q": {Instructions: "?"}}); err == nil {
		t.Error("a timed-out call must return an error")
	}
}

func TestNewFromEnvWithoutKey(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	t.Setenv("HOME", t.TempDir())
	if _, err := NewFromEnv(); err != ErrNoKey {
		t.Errorf("err = %v, want ErrNoKey", err)
	}
}

func TestRedact(t *testing.T) {
	// Fake keys are assembled at runtime so no key-shaped literal sits in
	// the source for secret scanners (or readers) to mistake for a real one.
	filler := strings.Repeat("X", 24)
	for _, secret := range []string{
		"sk-" + filler,
		"apikey_" + filler,
		"xai-" + filler,
		"ghp_" + filler,
		"AKIA" + strings.Repeat("X", 16),
		"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig",
		"Authorization: Basic dXNlcjpodW50ZXIyaHVudGVyMg==",
		"Proxy-Authorization: Token 9f8e7d6c5b4a39281706",
		"sk_" + "live_" + filler,
		"rk_" + "test_" + filler,
	} {
		if out := Redact("x " + secret + " y"); strings.Contains(out, secret) || !strings.Contains(out, "[REDACTED]") {
			t.Errorf("not redacted: %q -> %q", secret, out)
		}
	}
	if out := Redact(`"api_key": "hunter2hunter2"`); strings.Contains(out, "hunter2") || !strings.Contains(out, `"api_key"`) {
		t.Errorf("key=value: %q", out)
	}
	if out := Redact(`password=swordfish123`); out != "password=[REDACTED]" {
		t.Errorf("password: %q", out)
	}
	for in, leak := range map[string]string{
		"DATABASE_URL=postgres://admin:s3cretpw@db.internal:5432/app": "s3cretpw",
		"redis://:hunter2hunter2@cache:6379":                          "hunter2hunter2",
		"https://bot:ghs_tokenvalue123@github.com/org/repo.git":       "ghs_tokenvalue123",
		"MYSQL_PWD=rootpassword1":                                     "rootpassword1",
		"GCP_CREDENTIALS=eyJ0eXBlIjoic2VydmljZSJ9":                    "eyJ0eXBlIjoic2VydmljZSJ9",
		"X-Api-Key: 0123456789abcdef":                                 "0123456789abcdef",
		"x-auth-token: 0123456789abcdef":                              "0123456789abcdef",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAtruncated":  "MIIEowIBAAKCAQEAtruncated",
	} {
		if out := Redact(in); strings.Contains(out, leak) {
			t.Errorf("leaked %q: %q", leak, out)
		}
	}
	// Token counts are the LLM sense of "token", not credentials.
	for _, plain := range []string{
		"func Threshold(window int) int { return window - reserve }",
		"stats.InputTokens = resp.Usage.PromptTokens",
		"MatchedTokens:      matched,",
		"avgTokens = total / count",
	} {
		if Redact(plain) != plain {
			t.Errorf("ordinary code was altered: %q", Redact(plain))
		}
	}
	if out := Redact("refresh_token=abcdef123456"); strings.Contains(out, "abcdef123456") {
		t.Errorf("a singular token key must still be redacted: %q", out)
	}
}
