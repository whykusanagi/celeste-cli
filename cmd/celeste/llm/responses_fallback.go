package llm

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/sashabaranov/go-openai"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// apiStatus extracts the HTTP status, error code and message from a
// go-openai error. ok is false for errors that never got an HTTP answer.
func apiStatus(err error) (status int, code, msg string, ok bool) {
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		c, _ := apiErr.Code.(string)
		return apiErr.HTTPStatusCode, c, apiErr.Message, true
	}
	var reqErr *openai.RequestError
	if errors.As(err, &reqErr) {
		return reqErr.HTTPStatusCode, "", string(reqErr.Body), true
	}
	return 0, "", "", false
}

// isBlocksRejection reports an endpoint refusing replayed output items:
// reasoning whose encrypted_content no longer decrypts, or an item id it
// cannot resolve (ruling 10). A complaint about the request's include
// parameter, or that the model does not support encrypted content, is not
// one: the same request without the items would fail the same way.
func isBlocksRejection(err error) bool {
	status, _, msg, ok := apiStatus(err)
	if !ok || (status != http.StatusBadRequest && status != http.StatusNotFound) {
		return false
	}
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) && apiErr.Param != nil && strings.EqualFold(*apiErr.Param, "include") {
		return false
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "not supported with this model") {
		return false
	}
	for _, s := range []string{"encrypted_content", "encrypted content", "item with id", "reasoning item"} {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// BlocksRejectedError is a request's error when the provider refused the
// blocks it replayed and the resend without them failed too (W8-1 review
// M4). The refusal still holds: the caller strips the history's blocks
// (2.0 F3), as it does for a reply carrying BlocksRejected.
type BlocksRejectedError struct{ Err error }

func (e *BlocksRejectedError) Error() string { return e.Err.Error() }
func (e *BlocksRejectedError) Unwrap() error { return e.Err }

// BlocksRejectedIn reports whether err says the provider refused the
// replayed blocks (a *BlocksRejectedError anywhere in its chain).
func BlocksRejectedIn(err error) bool {
	var e *BlocksRejectedError
	return errors.As(err, &e)
}

// isUnsupportedEndpoint reports an endpoint that has no Responses API
// (ruling 8): 404 (but not a missing model or a refused item), 405, 501,
// or a 400 naming an unsupported endpoint or an invalid URL.
func isUnsupportedEndpoint(err error) bool {
	status, code, msg, ok := apiStatus(err)
	if !ok || isBlocksRejection(err) {
		return false
	}
	lower := strings.ToLower(msg)
	switch status {
	case http.StatusNotFound:
		return code != "model_not_found" && !(strings.Contains(lower, "model") && strings.Contains(lower, "does not exist"))
	case http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	case http.StatusBadRequest:
		return strings.Contains(lower, "unsupported endpoint") || strings.Contains(lower, "invalid url")
	}
	return false
}

// endpointKey normalizes a base URL for the per-endpoint memories.
func endpointKey(baseURL string) string {
	return strings.TrimRight(strings.ToLower(strings.TrimSpace(baseURL)), "/")
}

// responsesFallback holds the endpoints that turned out to have no
// Responses API, for the life of the process (ruling 8).
var responsesFallback sync.Map

func responsesFellBack(baseURL string) bool {
	_, ok := responsesFallback.Load(endpointKey(baseURL))
	return ok
}

// markResponsesFallback records the fallback and logs it once per endpoint.
func markResponsesFallback(baseURL string, cause error) {
	if _, loaded := responsesFallback.LoadOrStore(endpointKey(baseURL), struct{}{}); !loaded {
		tui.LogInfo(fallbackLogLine(baseURL, cause))
	}
}

// fallbackLogLine names the endpoint without credentials its base URL may
// carry.
func fallbackLogLine(baseURL string, cause error) string {
	return fmt.Sprintf("openai: %s has no Responses API (%v); using Chat Completions for this session", providers.CleanBaseURL(baseURL), cause)
}

// resetResponsesFallback forgets every fallback (tests).
func resetResponsesFallback() {
	responsesFallback.Range(func(k, _ any) bool {
		responsesFallback.Delete(k)
		return true
	})
}
