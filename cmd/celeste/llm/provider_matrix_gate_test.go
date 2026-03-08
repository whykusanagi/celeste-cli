package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/skills"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

type providerGateCase struct {
	Name     string
	Provider string
	Run      func(t *testing.T) error
}

type providerGateStats struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
}

type providerGateReport struct {
	GeneratedAt         time.Time                    `json:"generated_at"`
	TotalCases          int                          `json:"total_cases"`
	PassedCases         int                          `json:"passed_cases"`
	OverallPassRate     float64                      `json:"overall_pass_rate"`
	RequiredOverallRate float64                      `json:"required_overall_rate"`
	RequiredPerProvider float64                      `json:"required_per_provider"`
	Providers           map[string]providerGateStats `json:"providers"`
	Failures            []string                     `json:"failures,omitempty"`
}

type providerToolFixture struct {
	Tools []tui.SkillDefinition `json:"tools"`
}

type vertexRequiredFixture struct {
	Cases []struct {
		Name             string         `json:"name"`
		Schema           map[string]any `json:"schema"`
		ExpectedRequired []string       `json:"expected_required"`
	} `json:"cases"`
}

func TestProviderCompatibilityMatrixGate(t *testing.T) {
	t.Parallel()

	overallMin := parseGateThresholdEnv("CELESTE_PROVIDER_GATE_MIN_PASS", 1.0)
	perProviderMin := parseGateThresholdEnv("CELESTE_PROVIDER_GATE_MIN_PASS_PER_PROVIDER", 1.0)

	cases := []providerGateCase{
		{Name: "openai_tool_schema_parity", Provider: "openai", Run: runOpenAIToolSchemaParityCase},
		{Name: "openai_multi_tool_stream", Provider: "openai", Run: runOpenAIMultiToolStreamCase},
		{Name: "xai_tool_schema_parity", Provider: "xai", Run: runXAIToolSchemaParityCase},
		{Name: "xai_multi_tool_stream", Provider: "xai", Run: runXAIMultiToolStreamCase},
		{Name: "vertex_required_schema_forms", Provider: "vertex", Run: runVertexRequiredSchemaFormsCase},
		{Name: "backend_detection_openai_xai_vertex", Provider: "matrix", Run: runBackendDetectionMatrixCase},
	}

	if len(cases) == 0 {
		t.Fatalf("provider matrix gate has no cases")
	}

	stats := make(map[string]providerGateStats)
	failures := make([]string, 0)
	passed := 0

	for _, tc := range cases {
		entry := stats[tc.Provider]
		entry.Total++
		stats[tc.Provider] = entry

		if err := tc.Run(t); err != nil {
			failures = append(failures, fmt.Sprintf("[%s] %s: %v", tc.Provider, tc.Name, err))
			continue
		}

		entry = stats[tc.Provider]
		entry.Passed++
		stats[tc.Provider] = entry
		passed++
	}

	total := len(cases)
	overallRate := float64(passed) / float64(total)
	t.Logf("provider matrix gate: passed %d/%d (%.2f%%), required overall %.2f%%, required/provider %.2f%%",
		passed, total, overallRate*100.0, overallMin*100.0, perProviderMin*100.0)

	for provider, st := range stats {
		providerRate := 0.0
		if st.Total > 0 {
			providerRate = float64(st.Passed) / float64(st.Total)
		}
		t.Logf("provider matrix gate: provider=%s passed=%d/%d (%.2f%%)", provider, st.Passed, st.Total, providerRate*100.0)
		if providerRate < perProviderMin {
			failures = append(failures, fmt.Sprintf("provider threshold failed for %s: %.2f%% < %.2f%%", provider, providerRate*100.0, perProviderMin*100.0))
		}
	}

	report := providerGateReport{
		GeneratedAt:         time.Now().UTC(),
		TotalCases:          total,
		PassedCases:         passed,
		OverallPassRate:     overallRate,
		RequiredOverallRate: overallMin,
		RequiredPerProvider: perProviderMin,
		Providers:           stats,
		Failures:            failures,
	}
	writeProviderGateReportIfRequested(t, report)

	if overallRate < overallMin {
		failures = append(failures, fmt.Sprintf("overall threshold failed: %.2f%% < %.2f%%", overallRate*100.0, overallMin*100.0))
	}

	if len(failures) > 0 {
		t.Fatalf("provider matrix gate failed:\n%s", strings.Join(failures, "\n"))
	}
}

func runOpenAIToolSchemaParityCase(t *testing.T) error {
	fixture, err := loadProviderToolsFixture()
	if err != nil {
		return err
	}

	backend := NewOpenAIBackend(&Config{})
	tools := append([]tui.SkillDefinition{}, fixture.Tools...)
	tools = append(tools, tui.SkillDefinition{
		Name:        "invalid_tool",
		Description: "should be dropped",
		Parameters:  map[string]any{"bad": func() {}},
	})

	converted := backend.convertTools(tools)
	if len(converted) != len(fixture.Tools) {
		return fmt.Errorf("expected %d converted tools, got %d", len(fixture.Tools), len(converted))
	}

	for i, expected := range fixture.Tools {
		if converted[i].Function == nil {
			return fmt.Errorf("tool %d has nil function", i)
		}
		if converted[i].Function.Name != expected.Name {
			return fmt.Errorf("tool %d name mismatch: got %q want %q", i, converted[i].Function.Name, expected.Name)
		}
	}
	return nil
}

func runXAIToolSchemaParityCase(t *testing.T) error {
	fixture, err := loadProviderToolsFixture()
	if err != nil {
		return err
	}

	backend := &XAIBackend{}
	tools := append([]tui.SkillDefinition{}, fixture.Tools...)
	tools = append(tools, tui.SkillDefinition{
		Name:        "invalid_tool",
		Description: "should be dropped",
		Parameters:  map[string]any{"bad": func() {}},
	})

	converted := backend.convertTools(tools)
	if len(converted) != len(fixture.Tools) {
		return fmt.Errorf("expected %d converted tools, got %d", len(fixture.Tools), len(converted))
	}

	for i, expected := range fixture.Tools {
		if converted[i].Function.Name != expected.Name {
			return fmt.Errorf("tool %d name mismatch: got %q want %q", i, converted[i].Function.Name, expected.Name)
		}
		if len(converted[i].Function.Parameters) == 0 {
			return fmt.Errorf("tool %d has empty parameters", i)
		}
	}
	return nil
}

func runOpenAIMultiToolStreamCase(t *testing.T) error {
	fixture, err := loadProviderToolsFixture()
	if err != nil {
		return err
	}
	streamData, err := loadProviderFixtureText("openai_multi_tool_stream.sse")
	if err != nil {
		return err
	}

	requestToolCounts := make(chan int, 1)
	requestErrs := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			requestErrs <- fmt.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "bad method", http.StatusBadRequest)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			requestErrs <- fmt.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			requestErrs <- fmt.Errorf("decode request: %w", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if v, ok := payload["tools"].([]any); ok {
			requestToolCounts <- len(v)
		} else {
			requestToolCounts <- 0
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, streamData)
	}))
	defer server.Close()

	backend := NewOpenAIBackend(&Config{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "gpt-4o-mini",
	})
	messages := []tui.ChatMessage{{Role: "user", Content: "run tools"}}

	result, err := backend.SendMessageSync(context.Background(), messages, fixture.Tools)
	if err != nil {
		return fmt.Errorf("SendMessageSync: %w", err)
	}
	select {
	case reqErr := <-requestErrs:
		if reqErr != nil {
			return reqErr
		}
	default:
	}
	requestToolCount := -1
	select {
	case requestToolCount = <-requestToolCounts:
	default:
		return fmt.Errorf("request tool count not captured")
	}
	if requestToolCount != len(fixture.Tools) {
		return fmt.Errorf("request tools mismatch: got %d want %d", requestToolCount, len(fixture.Tools))
	}
	if result.FinishReason != "tool_calls" {
		return fmt.Errorf("finish reason mismatch: got %q", result.FinishReason)
	}
	if len(result.ToolCalls) != 2 {
		return fmt.Errorf("expected 2 tool calls, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "tool_alpha" || result.ToolCalls[1].Name != "tool_beta" {
		return fmt.Errorf("unexpected tool names: %+v", result.ToolCalls)
	}
	if !json.Valid([]byte(result.ToolCalls[0].Arguments)) || !json.Valid([]byte(result.ToolCalls[1].Arguments)) {
		return fmt.Errorf("tool arguments must be valid json: %+v", result.ToolCalls)
	}
	return nil
}

func runXAIMultiToolStreamCase(t *testing.T) error {
	fixture, err := loadProviderToolsFixture()
	if err != nil {
		return err
	}
	streamData, err := loadProviderFixtureText("xai_multi_tool_stream.sse")
	if err != nil {
		return err
	}

	requestToolCounts := make(chan int, 1)
	requestErrs := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			requestErrs <- fmt.Errorf("unexpected method: %s", r.Method)
			http.Error(w, "bad method", http.StatusBadRequest)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			requestErrs <- fmt.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			requestErrs <- fmt.Errorf("decode request: %w", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if v, ok := payload["tools"].([]any); ok {
			requestToolCounts <- len(v)
		} else {
			requestToolCounts <- 0
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, streamData)
	}))
	defer server.Close()

	backend, err := NewXAIBackend(&Config{
		APIKey:  "xai-test-key",
		BaseURL: server.URL,
		Model:   "grok-4-1-fast",
		Timeout: 1,
	}, skills.NewRegistry())
	if err != nil {
		return fmt.Errorf("NewXAIBackend: %w", err)
	}

	messages := []tui.ChatMessage{{Role: "user", Content: "run tools"}}
	var finalChunk StreamChunk
	err = backend.SendMessageStream(context.Background(), messages, fixture.Tools, func(chunk StreamChunk) {
		if chunk.IsFinal {
			finalChunk = chunk
		}
	})
	if err != nil {
		return fmt.Errorf("SendMessageStream: %w", err)
	}
	select {
	case reqErr := <-requestErrs:
		if reqErr != nil {
			return reqErr
		}
	default:
	}
	requestToolCount := -1
	select {
	case requestToolCount = <-requestToolCounts:
	default:
		return fmt.Errorf("request tool count not captured")
	}
	if requestToolCount != len(fixture.Tools) {
		return fmt.Errorf("request tools mismatch: got %d want %d", requestToolCount, len(fixture.Tools))
	}
	if finalChunk.FinishReason != "tool_calls" {
		return fmt.Errorf("finish reason mismatch: got %q", finalChunk.FinishReason)
	}
	if len(finalChunk.ToolCalls) != 2 {
		return fmt.Errorf("expected 2 tool calls, got %d", len(finalChunk.ToolCalls))
	}
	if finalChunk.ToolCalls[0].Name != "tool_alpha" || finalChunk.ToolCalls[1].Name != "tool_beta" {
		return fmt.Errorf("unexpected tool names: %+v", finalChunk.ToolCalls)
	}
	if !json.Valid([]byte(finalChunk.ToolCalls[0].Arguments)) || !json.Valid([]byte(finalChunk.ToolCalls[1].Arguments)) {
		return fmt.Errorf("tool arguments must be valid json: %+v", finalChunk.ToolCalls)
	}
	return nil
}

func runVertexRequiredSchemaFormsCase(t *testing.T) error {
	fixture, err := loadVertexRequiredFixture()
	if err != nil {
		return err
	}
	backend := &GoogleBackend{}

	for _, c := range fixture.Cases {
		schemaInput := deepCopyMap(c.Schema)
		requiredRaw, hasRequired := schemaInput["required"]
		if !hasRequired {
			return fmt.Errorf("case %s missing required field", c.Name)
		}

		if strings.Contains(c.Name, "string_slice") {
			requiredIface, ok := requiredRaw.([]any)
			if !ok {
				return fmt.Errorf("case %s required type mismatch: %T", c.Name, requiredRaw)
			}
			requiredStrings := make([]string, 0, len(requiredIface))
			for _, v := range requiredIface {
				if s, ok := v.(string); ok {
					requiredStrings = append(requiredStrings, s)
				}
			}
			schemaInput["required"] = requiredStrings
		}

		schema := backend.convertSchemaToGenAI(schemaInput)
		if schema == nil {
			return fmt.Errorf("case %s produced nil schema", c.Name)
		}
		if len(schema.Required) != len(c.ExpectedRequired) {
			return fmt.Errorf("case %s required length mismatch: got %v want %v", c.Name, schema.Required, c.ExpectedRequired)
		}
		for i := range c.ExpectedRequired {
			if schema.Required[i] != c.ExpectedRequired[i] {
				return fmt.Errorf("case %s required mismatch: got %v want %v", c.Name, schema.Required, c.ExpectedRequired)
			}
		}
	}
	return nil
}

func runBackendDetectionMatrixCase(t *testing.T) error {
	tests := []struct {
		URL      string
		Expected BackendType
	}{
		{URL: "https://api.openai.com/v1", Expected: BackendTypeOpenAI},
		{URL: "https://api.x.ai/v1", Expected: BackendTypeXAI},
		{URL: "https://generativelanguage.googleapis.com/v1beta", Expected: BackendTypeGoogle},
		{URL: "https://aiplatform.googleapis.com/v1/projects/x/locations/us-central1", Expected: BackendTypeGoogle},
	}

	for _, tc := range tests {
		got := DetectBackendType(tc.URL)
		if got != tc.Expected {
			return fmt.Errorf("DetectBackendType(%q)=%q want %q", tc.URL, got, tc.Expected)
		}
	}
	return nil
}

func loadProviderToolsFixture() (*providerToolFixture, error) {
	var fixture providerToolFixture
	if err := loadProviderFixtureJSON("tool_matrix.json", &fixture); err != nil {
		return nil, err
	}
	if len(fixture.Tools) == 0 {
		return nil, fmt.Errorf("tool fixture contains no tools")
	}
	return &fixture, nil
}

func loadVertexRequiredFixture() (*vertexRequiredFixture, error) {
	var fixture vertexRequiredFixture
	if err := loadProviderFixtureJSON("vertex_required_forms.json", &fixture); err != nil {
		return nil, err
	}
	if len(fixture.Cases) == 0 {
		return nil, fmt.Errorf("vertex required fixture contains no cases")
	}
	return &fixture, nil
}

func loadProviderFixtureJSON(name string, target any) error {
	data, err := os.ReadFile(filepath.Join("testdata", "provider_matrix", name))
	if err != nil {
		return fmt.Errorf("read fixture %s: %w", name, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("parse fixture %s: %w", name, err)
	}
	return nil
}

func loadProviderFixtureText(name string) (string, error) {
	data, err := os.ReadFile(filepath.Join("testdata", "provider_matrix", name))
	if err != nil {
		return "", fmt.Errorf("read fixture %s: %w", name, err)
	}
	return string(data), nil
}

func parseGateThresholdEnv(name string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	if parsed < 0 {
		return 0
	}
	if parsed > 1 {
		return 1
	}
	return parsed
}

func writeProviderGateReportIfRequested(t *testing.T, report providerGateReport) {
	t.Helper()
	path := strings.TrimSpace(os.Getenv("CELESTE_PROVIDER_GATE_REPORT"))
	if path == "" {
		return
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Logf("provider gate: failed to serialize report: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Logf("provider gate: failed to write report %s: %v", path, err)
		return
	}
	t.Logf("provider gate report written: %s", path)
}

func deepCopyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}
