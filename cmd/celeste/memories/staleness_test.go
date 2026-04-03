package memories

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCheckStalenessFresh(t *testing.T) {
	m := &Memory{
		Name:    "fresh",
		Created: time.Now().Add(-2 * 24 * time.Hour).Format(time.RFC3339),
	}
	days, warning := CheckStaleness(m)
	assert.Equal(t, 2, days)
	assert.Empty(t, warning)
}

func TestCheckStalenessModerate(t *testing.T) {
	m := &Memory{
		Name:    "moderate",
		Created: time.Now().Add(-14 * 24 * time.Hour).Format(time.RFC3339),
	}
	days, warning := CheckStaleness(m)
	assert.Equal(t, 14, days)
	assert.Contains(t, warning, "consider verifying")
}

func TestCheckStalenessOld(t *testing.T) {
	m := &Memory{
		Name:    "old",
		Created: time.Now().Add(-60 * 24 * time.Hour).Format(time.RFC3339),
	}
	days, warning := CheckStaleness(m)
	assert.Equal(t, 60, days)
	assert.Contains(t, warning, "outdated")
}

func TestCheckStalenessInvalidDate(t *testing.T) {
	m := &Memory{Name: "bad", Created: "not-a-date"}
	days, warning := CheckStaleness(m)
	assert.Equal(t, 0, days)
	assert.Empty(t, warning)
}

func TestShouldVerifyTrue(t *testing.T) {
	m := &Memory{
		Created: time.Now().Add(-10 * 24 * time.Hour).Format(time.RFC3339),
	}
	assert.True(t, ShouldVerify(m))
}

func TestShouldVerifyFalse(t *testing.T) {
	m := &Memory{
		Created: time.Now().Add(-3 * 24 * time.Hour).Format(time.RFC3339),
	}
	assert.False(t, ShouldVerify(m))
}

func TestShouldVerifyInvalidDate(t *testing.T) {
	m := &Memory{Created: "invalid"}
	assert.False(t, ShouldVerify(m))
}
