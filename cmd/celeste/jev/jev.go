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
	// Workspace makes paths inside it workspace-relative in what Ask sends;
	// every other absolute or ~ path is sent as <path> (RedactPaths).
	Workspace string
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

// Question is one typed System One question: "noul" (yes/no), "choice"
// (Criteria maps each option to its description, nil for none) or "score"
// (Criteria is the ordered list of level descriptions, 2 to 10).
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// Answer is one typed answer. Noul carries P(yes); Choice the most likely
// option; Score the probability-weighted level (0-based, can fall between
// levels). Probabilities and Confidence come with choice and score answers.
type Answer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
}

// Ask asks every question against state in one request and returns the
// answers by question id, with the response's model version for logging.
// The API shape was checked against https://docs.typesafe.ai/api.md
// (Choice, Score, Noul) on 2026-10-01.
func (c *Client) Ask(ctx context.Context, state any, qs map[string]Question) (map[string]Answer, string, error) {
	if len(qs) == 0 {
		return nil, "", nil
	}
	model := c.Model
	if model == "" {
		model = DefaultModel
	}
	// The state goes to a third party: secrets and file paths are redacted
	// here, whatever the caller already did (2.0 W3).
	state = RedactValue(state, c.Workspace)
	body, err := json.Marshal(map[string]any{"state": state, "model": model, "questions": qs})
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
		Model   string            `json:"model"`
		Answers map[string]Answer `json:"answers"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, "", fmt.Errorf("jev: bad response: %w", err)
	}
	return out.Answers, out.Model, nil
}

// Nouls asks yes/no questions only and returns P(yes) per question id.
func (c *Client) Nouls(ctx context.Context, state any, qs map[string]Noul) (map[string]float64, string, error) {
	if len(qs) == 0 {
		return nil, "", nil
	}
	wire := make(map[string]Question, len(qs))
	for id, q := range qs {
		w := Question{Type: "noul", Instructions: q.Instructions}
		if q.True != "" || q.False != "" {
			w.Criteria = map[string]string{"true": q.True, "false": q.False}
		}
		wire[id] = w
	}
	answers, model, err := c.Ask(ctx, state, wire)
	if err != nil {
		return nil, "", err
	}
	probs := make(map[string]float64, len(answers))
	for id, a := range answers {
		if a.Noul != nil {
			probs[id] = *a.Noul
		}
	}
	return probs, model, nil
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
