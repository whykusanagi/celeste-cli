package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// CurrencyTool converts between currencies.
type CurrencyTool struct {
	BaseTool
}

// NewCurrencyTool creates a CurrencyTool.
func NewCurrencyTool() *CurrencyTool {
	return &CurrencyTool{
		BaseTool: BaseTool{
			ToolName:        "convert_currency",
			ToolDescription: "Convert between different currencies using current exchange rates",
			ToolParameters: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"amount": map[string]any{
						"type":        "number",
						"description": "Amount to convert",
					},
					"from_currency": map[string]any{
						"type":        "string",
						"description": "Source currency code (e.g., 'USD', 'EUR', 'JPY', 'GBP')",
					},
					"to_currency": map[string]any{
						"type":        "string",
						"description": "Target currency code (e.g., 'USD', 'EUR', 'JPY', 'GBP')",
					},
				},
				"required": []string{"amount", "from_currency", "to_currency"},
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"amount", "from_currency", "to_currency"},
		},
	}
}

func (t *CurrencyTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	amount, ok := input["amount"].(float64)
	if !ok {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'amount' parameter must be a number",
			"Please provide a numeric amount to convert.",
			map[string]any{
				"skill": "convert_currency",
				"field": "amount",
			},
		))
	}

	fromCurrency, ok := input["from_currency"].(string)
	if !ok || fromCurrency == "" {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'from_currency' parameter is required",
			"Please specify the source currency code (e.g., 'USD', 'EUR', 'JPY', 'GBP').",
			map[string]any{
				"skill": "convert_currency",
				"field": "from_currency",
			},
		))
	}

	toCurrency, ok := input["to_currency"].(string)
	if !ok || toCurrency == "" {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'to_currency' parameter is required",
			"Please specify the target currency code (e.g., 'USD', 'EUR', 'JPY', 'GBP').",
			map[string]any{
				"skill": "convert_currency",
				"field": "to_currency",
			},
		))
	}

	fromCurrency = strings.ToUpper(fromCurrency)
	toCurrency = strings.ToUpper(toCurrency)

	if fromCurrency == toCurrency {
		return resultFromMap(map[string]any{
			"amount":        amount,
			"from_currency": fromCurrency,
			"to_currency":   toCurrency,
			"converted":     amount,
			"rate":          1.0,
		})
	}

	url := fmt.Sprintf("https://api.exchangerate-api.com/v6/latest/%s", fromCurrency)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"network_error",
			"Failed to connect to currency API",
			"Please check your internet connection and try again.",
			map[string]any{
				"skill": "convert_currency",
				"error": err.Error(),
			},
		))
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return resultFromMap(formatErrorResponse(
			"api_error",
			fmt.Sprintf("Currency API returned error (status %d)", resp.StatusCode),
			"The currency exchange service may be temporarily unavailable. Please try again later.",
			map[string]any{
				"skill":       "convert_currency",
				"status_code": resp.StatusCode,
				"response":    string(body),
			},
		))
	}

	var result struct {
		Rates map[string]float64 `json:"rates"`
		Base  string             `json:"base"`
		Date  string             `json:"date"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return resultFromMap(formatErrorResponse(
			"api_error",
			"Failed to parse currency API response",
			"The currency service returned invalid data. Please try again.",
			map[string]any{
				"skill": "convert_currency",
				"error": err.Error(),
			},
		))
	}

	rate, ok := result.Rates[toCurrency]
	if !ok {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Currency %s not found in exchange rates", toCurrency),
			"Please use a valid 3-letter currency code (e.g., USD, EUR, JPY, GBP)",
			map[string]any{
				"skill":    "convert_currency",
				"field":    "to_currency",
				"provided": toCurrency,
			},
		))
	}

	converted := amount * rate

	return resultFromMap(map[string]any{
		"amount":        amount,
		"from_currency": fromCurrency,
		"to_currency":   toCurrency,
		"converted":     converted,
		"rate":          rate,
		"date":          result.Date,
	})
}
