//go:build !cgo

// CGo-disabled fallback for the TypeScript parser. Compiled when
// CGO_ENABLED=0 — typically the cross-compile release build when
// targeting darwin/windows from a Linux host without a C toolchain
// installed. The real tree-sitter implementation lives in
// parser_ts_cgo.go behind a //go:build cgo constraint.
//
// The stub implements the same TSParser API as the CGo version but
// delegates every ParseFile call to the regex GenericParser. Result:
// a CGo-disabled build still indexes .ts and .tsx files end-to-end,
// just without the accurate call_expression edge resolution that
// tree-sitter provides. Zero-edge interface noise reappears in the
// top-10 results for TypeScript-heavy projects — the v1.8.x behavior
// that the CGo build improves upon — but search doesn't break.
//
// Keep this file's exported API 1:1 with parser_ts_cgo.go. Any time
// the CGo version gains a new method, add the stub equivalent here
// or the build will fail under the opposite constraint.
package codegraph

// TSParser is the CGo-disabled stub. It holds no native resources;
// the underlying GenericParser is created per ParseFile call to
// match the cached-parser semantics the indexFile dispatch expects.
type TSParser struct{}

// NewTSParser returns a stub TSParser. The real implementation is
// in parser_ts_cgo.go; this file is only compiled when CGO_ENABLED=0.
func NewTSParser() *TSParser {
	return &TSParser{}
}

// Close is a no-op in the stub — nothing to release.
func (p *TSParser) Close() {}

// ParseFile falls back to the regex GenericParser for .ts and .tsx
// files. Lang is hard-coded to "typescript" because the stub only
// ever receives TS/TSX files from indexFile's dispatch (.tsx falls
// through the same extension check as .ts in GenericParser).
func (p *TSParser) ParseFile(path string) (*ParseResult, error) {
	return NewGenericParser("typescript").ParseFile(path)
}
