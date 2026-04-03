package memories

import (
	"fmt"
	"time"
)

// CheckStaleness returns how many days old a memory is and a warning string.
// If the memory is fresh (< 7 days), the warning is empty.
func CheckStaleness(memory *Memory) (int, string) {
	created, err := time.Parse(time.RFC3339, memory.Created)
	if err != nil {
		return 0, ""
	}

	days := int(time.Since(created).Hours() / 24)

	if days <= 7 {
		return days, ""
	}

	if days <= 30 {
		return days, fmt.Sprintf("memory '%s' is %d days old, consider verifying it is still accurate", memory.Name, days)
	}

	return days, fmt.Sprintf("memory '%s' is %d days old and may be outdated, consider refreshing or deleting it", memory.Name, days)
}

// ShouldVerify returns true if a memory is older than 7 days.
func ShouldVerify(memory *Memory) bool {
	created, err := time.Parse(time.RFC3339, memory.Created)
	if err != nil {
		return false
	}
	return time.Since(created).Hours() > 7*24
}
