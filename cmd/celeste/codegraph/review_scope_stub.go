//go:build !cgo

package codegraph

// treeSitterSpans has no parser without cgo: every non-Go function uses the
// text-scan fallback.
func (r *reviewer) treeSitterSpans(_ string, _ []byte) ([]funcSpan, bool) {
	return nil, false
}
