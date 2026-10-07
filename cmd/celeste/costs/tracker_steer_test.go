package costs

import "testing"

// RecordCost adds tokens and cost without counting a turn (a reply a stream
// rule dropped, 2.0 W3).
func TestRecordCostCountsNoTurn(t *testing.T) {
	tr := NewSessionTracker()
	tr.RecordUsage("gpt-4.1", Usage{Input: 100, Output: 10})
	tr.RecordCost("gpt-4.1", Usage{Input: 100, Output: 10})
	s := tr.GetSummary()
	if s.Turns != 1 || s.TotalInput != 200 || s.TotalOutput != 20 || s.TotalCostUSD <= GetCost("gpt-4.1", 100, 10) {
		t.Errorf("summary = %+v", s)
	}
}
