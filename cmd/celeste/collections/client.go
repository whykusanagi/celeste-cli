package collections

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

const defaultManagementAPIURL = "https://management-api.x.ai/v1"

// Client handles xAI Collections Management API operations
type Client struct {
	managementAPIKey string
	baseURL          string
	httpClient       *http.Client
}

// NewClient creates a new Collections API client
func NewClient(managementAPIKey string) *Client {
	return &Client{
		managementAPIKey: managementAPIKey,
		baseURL:          defaultManagementAPIURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// CreateCollection creates a new collection
func (c *Client) CreateCollection(name, description string) (string, error) {
	url := c.baseURL + "/collections"

	// Build request body
	body := map[string]string{
		"collection_name": name,
		"description":     description,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.managementAPIKey)
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Check status
	if resp.StatusCode != http.StatusOK {
		return "", &CollectionsError{
			StatusCode: resp.StatusCode,
			Message:    string(respBodyBytes),
			RequestID:  resp.Header.Get("X-Request-ID"),
		}
	}

	// Parse response
	var result map[string]interface{}
	if err := json.Unmarshal(respBodyBytes, &result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w (body: %s)", err, string(respBodyBytes))
	}

	collectionIDInterface, ok := result["collection_id"]
	if !ok {
		return "", fmt.Errorf("collection_id not found in response")
	}

	collectionID, ok := collectionIDInterface.(string)
	if !ok {
		return "", fmt.Errorf("collection_id is not a string: %v", collectionIDInterface)
	}

	return collectionID, nil
}

// ListCollections lists all collections
func (c *Client) ListCollections() ([]Collection, error) {
	url := c.baseURL + "/collections"

	// Create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.managementAPIKey)

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, &CollectionsError{
			StatusCode: resp.StatusCode,
			Message:    string(bodyBytes),
			RequestID:  resp.Header.Get("X-Request-ID"),
		}
	}

	// Parse response
	var result struct {
		Collections []Collection `json:"collections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Collections, nil
}

// UploadDocument uploads a document to a collection
func (c *Client) UploadDocument(collectionID, name string, data []byte, contentType string) (string, error) {
	url := c.baseURL + "/collections/" + collectionID + "/documents"

	// Create multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add name field
	if err := writer.WriteField("name", name); err != nil {
		return "", fmt.Errorf("failed to write name field: %w", err)
	}

	// Add content_type field
	if err := writer.WriteField("content_type", contentType); err != nil {
		return "", fmt.Errorf("failed to write content_type field: %w", err)
	}

	// Add data file
	part, err := writer.CreateFormFile("data", name)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("failed to write file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close writer: %w", err)
	}

	// Create request
	req, err := http.NewRequest("POST", url, &buf)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.managementAPIKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", &CollectionsError{
			StatusCode: resp.StatusCode,
			Message:    string(bodyBytes),
			RequestID:  resp.Header.Get("X-Request-ID"),
		}
	}

	// Parse response
	var result struct {
		FileMetadata struct {
			FileID string `json:"file_id"`
		} `json:"file_metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.FileMetadata.FileID, nil
}

// DeleteCollection deletes a collection
func (c *Client) DeleteCollection(collectionID string) error {
	url := c.baseURL + "/collections/" + collectionID

	// Create request
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.managementAPIKey)

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return &CollectionsError{
			StatusCode: resp.StatusCode,
			Message:    string(bodyBytes),
			RequestID:  resp.Header.Get("X-Request-ID"),
		}
	}

	return nil
}
