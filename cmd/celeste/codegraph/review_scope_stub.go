//go:build !cgo

package codegraph

// treeSitterSpans has no parser without cgo: every non-Go function uses the
// text-scan fallback.
func (r *reviewer) treeSitterSpans(_ string, _ []byte) ([]funcSpan, bool) {
	return nil, false
}

// declLanguage is false without cgo: the text-scan fallback records no
// class member declarations.
func declLanguage(_ string) bool { return false }
