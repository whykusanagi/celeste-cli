// cmd/celeste/permissions/denial_test.go
package permissions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDenialTracker_RecordAndCount(t *testing.T) {
	tracker := NewDenialTracker()

	assert.Equal(t, 0, tracker.GetDenialCount("bash"))
	assert.Equal(t, 0, tracker.GetTotalDenials())

	tracker.RecordDenial("bash")
	assert.Equal(t, 1, tracker.GetDenialCount("bash"))
	assert.Equal(t, 1, tracker.GetTotalDenials())

	tracker.RecordDenial("bash")
	tracker.RecordDenial("bash")
	assert.Equal(t, 3, tracker.GetDenialCount("bash"))
	assert.Equal(t, 3, tracker.GetTotalDenials())
}

func TestDenialTracker_MultipleTtools(t *testing.T) {
	tracker := NewDenialTracker()

	tracker.RecordDenial("bash")
	tracker.RecordDenial("bash")
	tracker.RecordDenial("write_file")

	assert.Equal(t, 2, tracker.GetDenialCount("bash"))
	assert.Equal(t, 1, tracker.GetDenialCount("write_file"))
	assert.Equal(t, 3, tracker.GetTotalDenials())
}

func TestDenialTracker_ShouldSuggestRule(t *testing.T) {
	tracker := NewDenialTracker()

	// Should not suggest before 3 denials
	tracker.RecordDenial("bash")
	tracker.RecordDenial("bash")
	assert.False(t, tracker.ShouldSuggestRule("bash"))

	// Should suggest at exactly 3
	tracker.RecordDenial("bash")
	assert.True(t, tracker.ShouldSuggestRule("bash"))

	// Should still suggest after 3
	tracker.RecordDenial("bash")
	assert.True(t, tracker.ShouldSuggestRule("bash"))

	// Different tool should not be affected
	assert.False(t, tracker.ShouldSuggestRule("write_file"))
}

func TestDenialTracker_ShouldSuggestStrictMode(t *testing.T) {
	tracker := NewDenialTracker()

	// Should not suggest before 5 total denials
	for i := 0; i < 4; i++ {
		tracker.RecordDenial("tool" + string(rune('A'+i)))
	}
	assert.False(t, tracker.ShouldSuggestStrictMode())

	// Should suggest at exactly 5
	tracker.RecordDenial("toolE")
	assert.True(t, tracker.ShouldSuggestStrictMode())

	// Should still suggest after 5
	tracker.RecordDenial("toolF")
	assert.True(t, tracker.ShouldSuggestStrictMode())
}

func TestDenialTracker_Reset(t *testing.T) {
	tracker := NewDenialTracker()

	tracker.RecordDenial("bash")
	tracker.RecordDenial("bash")
	tracker.RecordDenial("bash")
	assert.Equal(t, 3, tracker.GetTotalDenials())

	tracker.Reset()
	assert.Equal(t, 0, tracker.GetTotalDenials())
	assert.Equal(t, 0, tracker.GetDenialCount("bash"))
	assert.False(t, tracker.ShouldSuggestRule("bash"))
}

func TestDenialTracker_ResetTool(t *testing.T) {
	tracker := NewDenialTracker()

	tracker.RecordDenial("bash")
	tracker.RecordDenial("bash")
	tracker.RecordDenial("bash")
	tracker.RecordDenial("write_file")

	tracker.ResetTool("bash")
	assert.Equal(t, 0, tracker.GetDenialCount("bash"))
	assert.Equal(t, 1, tracker.GetDenialCount("write_file"))
	assert.Equal(t, 1, tracker.GetTotalDenials())
}

func TestDenialTracker_ConcurrencySafe(t *testing.T) {
	tracker := NewDenialTracker()
	done := make(chan struct{})

	// Run concurrent denials — should not panic
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				tracker.RecordDenial("bash")
				tracker.GetDenialCount("bash")
				tracker.GetTotalDenials()
				tracker.ShouldSuggestRule("bash")
				tracker.ShouldSuggestStrictMode()
			}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	assert.Equal(t, 1000, tracker.GetTotalDenials())
}
