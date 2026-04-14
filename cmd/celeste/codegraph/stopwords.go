// Stopwords runtime integration — embeds stopwords.json at build time
// and exposes parsed lookup sets for ShinglesForSymbol and SemanticSearch
// to consume.
//
// The embedded file is produced by celeste-stopwords and licensed under
// CC BY 4.0 — see stopwords_NOTICE.md for attribution.
package codegraph

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

//go:embed stopwords.json
var stopWordsRaw []byte

// StopWords holds the parsed lookup sets used at shingle-generation time
// and query-tokenization time. Built once at init.
type StopWords struct {
	Version   string
	Universal map[string]bool
	ByLang    map[string]map[string]bool
	Compound  map[string]bool
}

// IsCompound returns true if the lowercased identifier is in the
// compound_identifiers list. Used by splitIdentifier to keep known
// compound names (jquery, github, mysql, ...) atomic instead of
// decomposing them into parts that pollute searches.
//
// The splitCamelCase fix in v1.9.0 (min-3-uppercase rule) already
// handles most compound-name cases structurally, so this lookup is
// a belt-and-suspenders layer: it catches snake_case compounds
// (mysql_config → would split to ["mysql", "config"] without this,
// but we WANT "mysql" to stay atomic because splitting to "my"+"sql"
// or similar is worse) and any lowercase-only compounds that
// splitCamelCase wouldn't touch at all.
func (s *StopWords) IsCompound(name string) bool {
	if s == nil {
		return false
	}
	return s.Compound[strings.ToLower(name)]
}

// Filter removes any tokens in the universal set or in the per-language
// set for the given lang from the input slice. Empty lang means
// universal-only filtering. The input slice is NOT mutated.
//
// Preserves the order of surviving tokens. Returns a freshly allocated
// slice (safe for the caller to hold).
func (s *StopWords) Filter(tokens []string, lang string) []string {
	if s == nil || len(tokens) == 0 {
		return tokens
	}
	out := make([]string, 0, len(tokens))
	langSet := s.ByLang[lang]
	for _, t := range tokens {
		if s.Universal[t] {
			continue
		}
		if langSet != nil && langSet[t] {
			continue
		}
		out = append(out, t)
	}
	return out
}

// UniversalSize returns the number of universal stop words. Used by
// the anchor test to assert the embedded file isn't obviously broken.
func (s *StopWords) UniversalSize() int {
	if s == nil {
		return 0
	}
	return len(s.Universal)
}

// stopWords is the package-global lookup built at init. Callers read
// this directly rather than passing it through every function. It may
// be nil if the embedded file is malformed — callers must nil-check.
// In practice init panics on malformed input, so the nil branch is
// defensive rather than reachable.
var stopWords *StopWords

func init() {
	s, err := parseStopWords(stopWordsRaw)
	if err != nil {
		// Panic at init time so a corrupt embedded stopwords.json is a
		// build-time error (the binary won't start), not a runtime
		// surprise. Celeste's build process should catch this.
		panic(fmt.Errorf("stopwords.json: embedded file is corrupt: %w", err))
	}
	if len(s.Universal) == 0 {
		log.Printf("warning: stopwords.json loaded but universal set is empty — filter will be a no-op")
	}
	stopWords = s
}

// stopWordsFile is the on-disk JSON shape produced by celeste-stopwords'
// cmd/export. We only unmarshal the fields we consume; license /
// attribution / corpus blocks are read verbatim into interface{} fields
// so they round-trip but aren't otherwise touched.
type stopWordsFile struct {
	Version             string              `json:"version"`
	StopWords           map[string][]string `json:"stop_words"`
	CompoundIdentifiers []string            `json:"compound_identifiers"`
}

func parseStopWords(raw []byte) (*StopWords, error) {
	var f stopWordsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	s := &StopWords{
		Version:   f.Version,
		Universal: toLowerSet(f.StopWords["universal"]),
		ByLang:    make(map[string]map[string]bool, len(f.StopWords)),
		Compound:  toLowerSet(f.CompoundIdentifiers),
	}
	for lang, toks := range f.StopWords {
		if lang == "universal" {
			continue
		}
		s.ByLang[lang] = toLowerSet(toks)
	}
	return s, nil
}

// toLowerSet normalizes an input slice into a lowercase lookup set.
// Empty strings are dropped.
func toLowerSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		lower := strings.ToLower(strings.TrimSpace(s))
		if lower != "" {
			m[lower] = true
		}
	}
	return m
}
