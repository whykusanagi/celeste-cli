package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// KindRepoStreamRules is a repo grimoire's "## Stream Rules" section (2.0
// W3). It runs nothing, but its rules can stop replies, force paid re-runs
// and add reminders the model treats as authoritative, so it is trusted
// like a repo hook: by content hash, in the same store, with the same
// `celeste hooks trust` flow. Load never runs or warns about it; the rules
// package checks its trust (rules.Trusted).
const KindRepoStreamRules SourceKind = "repo-stream-rules"

// streamRulesSuffix keeps a grimoire's stream rules apart from its v1 hooks
// in the trust store, which keys by path.
const streamRulesSuffix = "#stream-rules"

// StreamRulesSource is the trust source for one grimoire file's "## Stream
// Rules" section: keyed by the file's path plus "#stream-rules", hashed
// over the section text, so any edit asks again.
func StreamRulesSource(grimoirePath, body string) Source {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	sum := sha256.Sum256([]byte(body))
	return Source{
		Path:  grimoirePath + streamRulesSuffix,
		Root:  grimoireRoot(grimoirePath),
		Kind:  KindRepoStreamRules,
		Rules: body,
		Hash:  hex.EncodeToString(sum[:]),
	}
}
