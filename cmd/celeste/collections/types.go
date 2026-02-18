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
	DocumentCount int       `json:"document_count,omitempty"`
}

// Document represents a document in a collection
type Document struct {
	FileID      string    `json:"file_id"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	UploadedAt  time.Time `json:"uploaded_at"`
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
