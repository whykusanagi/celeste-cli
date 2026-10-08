//go:build !cgo

// Stub for non-CGo builds. MultiLangParser is unavailable without CGo;
// all languages fall back to the regex GenericParser.
package codegraph

// MultiLangParser stub — unavailable without CGo.
type MultiLangParser struct{}

func NewMultiLangParser() *MultiLangParser { return &MultiLangParser{} }
func (m *MultiLangParser) Close()          {}
func (m *MultiLangParser) ParseFile(path string) (*ParseResult, error) {
	return NewGenericParser("unknown").ParseFile(path)
}
func (m *MultiLangParser) ParseSource(path string, data []byte) (*ParseResult, error) {
	return NewGenericParser("unknown").ParseSource(path, data)
}

// tryMultiParser always returns false in non-CGo builds.
func (idx *Indexer) tryMultiParser(_ string) bool {
	return false
}
