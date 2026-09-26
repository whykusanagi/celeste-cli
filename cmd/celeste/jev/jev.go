// Package jev is a minimal client for TypeSafe's System One API (Jev), used as
// an optional judge where celeste's own rules are a guess (#175). Every caller
// must treat an error as "no opinion" and fall back to its rules.
//
// API: POST https://api.typesafe.ai/v1/systemone, verified against the live
// docs and endpoint on 2026-09-25. A missing key returns 403, not the 401 the
// docs list.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	defaultURL   = "https://api.typesafe.ai/v1/systemone"
	DefaultModel = "jev-latest"
	// Timeout bounds every call; callers fall back to their rules after it.
	// Measured latency is ~0.2-0.45 s for up to ~11k input tokens.
	Timeout = 2500 * time.Millisecond
)

// Noul is a yes/no question; the answer is P(yes).
type Noul struct {
	Instructions string
	True, False  string // optional criteria: what yes and no mean
}

// Client calls the System One endpoint.
type Client struct {
	Key   string
	Model string
	URL   string
	HTTP  *http.Client
}

// ErrNoKey means no key is configured; Jev stays off.
var ErrNoKey = errors.New("jev: no TypeSafe API key (set TYPESAFE_API_KEY or ~/.celeste/typesafe.key)")

// NewFromEnv loads the key from TYPESAFE_API_KEY, else ~/.celeste/typesafe.key.
func NewFromEnv() (*Client, error) {
	key := strings.TrimSpace(os.Getenv("TYPESAFE_API_KEY"))
	if key == "" {
		if home, err := os.UserHomeDir(); err == nil {
			if b, err := os.ReadFile(filepath.Join(home, ".celeste", "typesafe.key")); err == nil {
				key = strings.TrimSpace(string(b))
			}
		}
	}
	if key == "" {
		return nil, ErrNoKey
	}
	return &Client{Key: key}, nil
}

type wireNoul struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
}

// Nouls asks every question against state in one request and returns P(yes)
// per question id. The response's model version is returned for logging.
func (c *Client) Nouls(ctx context.Context, state any, qs map[string]Noul) (map[string]float64, string, error) {
	if len(qs) == 0 {
		return nil, "", nil
	}
	wire := make(map[string]wireNoul, len(qs))
	for id, q := range qs {
		w := wireNoul{Type: "noul", Instructions: q.Instructions}
		if q.True != "" || q.False != "" {
			w.Criteria = map[string]string{"true": q.True, "false": q.False}
		}
		wire[id] = w
	}
	model := c.Model
	if model == "" {
		model = DefaultModel
	}
	body, err := json.Marshal(map[string]any{"state": state, "model": model, "questions": wire})
	if err != nil {
		return nil, "", err
	}
	url := c.URL
	if url == "" {
		url = defaultURL
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("jev: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		// ponytail: no retry on 429/529; the caller's rules are the fallback.
		return nil, "", fmt.Errorf("jev: HTTP %d: %.300s", resp.StatusCode, raw)
	}
	var out struct {
		Model   string `json:"model"`
		Answers map[string]struct {
			Noul *float64 `json:"noul"`
		} `json:"answers"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, "", fmt.Errorf("jev: bad response: %w", err)
	}
	probs := make(map[string]float64, len(out.Answers))
	for id, a := range out.Answers {
		if a.Noul != nil {
			probs[id] = *a.Noul
		}
	}
	return probs, out.Model, nil
}

// secretPatterns catches common key formats and key=value secrets. Excerpts
// sent to Jev go to a third party, so they are redacted first.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(sk|pk|rk)[-_][A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`\b(apikey|xai|ghp|gho|ghs|github_pat|glpat|xox[abpr])[-_][A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}`),
	// A PEM block, or its start when the text was already cut before the END line.
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(-----END [A-Z ]*PRIVATE KEY-----|$)`),
	regexp.MustCompile(`(?i)\b(bearer|basic|token)\s+[A-Za-z0-9._~+/=-]{12,}`),
}

// urlUserinfo matches user:pass@ in any URL (database URLs, git remotes).
var urlUserinfo = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^\s/@:]*:[^\s/@]+@`)

// keyValuePattern redacts the value after any key whose name suggests a secret.
var keyValuePattern = regexp.MustCompile(`(?i)\b([A-Za-z0-9_-]*(api[_-]?key|secret|token|passw(or)?d|pwd|credential|auth|private[_-]?key|access[_-]?key)[A-Za-z0-9_-]*)(["']?\s*[:=]\s*["']?)[^\s"',}]{6,}`)

// Redact replaces likely secrets with [REDACTED]. It errs toward redacting:
// a lost word costs Jev a little accuracy, a leaked key costs much more.
func Redact(s string) string {
	// Specific token shapes first: the generic key=value rule below would
	// otherwise take "Authorization: Bearer" and stop at "Bearer".
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	s = urlUserinfo.ReplaceAllString(s, "${1}[REDACTED]@")
	return keyValuePattern.ReplaceAllStringFunc(s, func(m string) string {
		g := keyValuePattern.FindStringSubmatch(m)
		if strings.Contains(strings.ToLower(g[1]), "tokens") {
			return m // a token count (InputTokens, avgTokens), not a credential
		}
		return g[1] + g[4] + "[REDACTED]"
	})
}
