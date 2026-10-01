package collections

import (
	"fmt"
	"time"
)

// Collection represents an xAI collection
type Collection struct {
	ID            string    `json:"collection_id"`
	Name          string    `json:"collection_name"`
	Description   string    `json:"description"`
	CreatedAt     time.Time `json:"created_at"`
	DocumentCount int       `json:"documents_count,omitempty"`
	TotalFileSize string    `json:"total_file_size,omitempty"`
}

// CollectionsError represents an API error
type CollectionsError struct {
	StatusCode int
	Message    string
	RequestID  string
}

func (e *CollectionsError) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("collections API error (status %d, request %s): %s",
			e.StatusCode, e.RequestID, e.Message)
	}
	return fmt.Sprintf("collections API error (status %d): %s", e.StatusCode, e.Message)
}
