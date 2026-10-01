package rules

import (
	"strings"
	"testing"
)

// benchReply streams an n-byte reply that fires no rule in 4-byte deltas
// through the built-ins, as a chat reply does (W3-1 review I1).
func benchReply(b *testing.B, n int) {
	reply := strings.Repeat("plain words, no rule here. ", n/27+1)[:n]
	set := &Set{Rules: Builtins()}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := NewMatcher(set)
		m.StartRequest()
		for j := 0; j < len(reply); j += 4 {
			m.Text(reply[j:min(j+4, len(reply))])
		}
		m.Flush()
	}
}

func BenchmarkTextReply8KB(b *testing.B)  { benchReply(b, 8<<10) }
func BenchmarkTextReply40KB(b *testing.B) { benchReply(b, 40<<10) }
