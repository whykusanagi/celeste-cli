package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// UnitConverterTool converts between units of measurement.
type UnitConverterTool struct {
	BaseTool
}

// NewUnitConverterTool creates a UnitConverterTool.
func NewUnitConverterTool() *UnitConverterTool {
	return &UnitConverterTool{
		BaseTool: BaseTool{
			ToolName:        "convert_units",
			ToolDescription: "Convert between different units of measurement (length, weight, temperature, volume)",
			ToolParameters: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"value": map[string]any{
						"type":        "number",
						"description": "The numeric value to convert",
					},
					"from_unit": map[string]any{
						"type":        "string",
						"description": "Source unit (e.g., 'm', 'km', 'ft', 'mi', 'kg', 'lb', 'celsius', 'fahrenheit', 'liter', 'gallon')",
					},
					"to_unit": map[string]any{
						"type":        "string",
						"description": "Target unit (e.g., 'm', 'km', 'ft', 'mi', 'kg', 'lb', 'celsius', 'fahrenheit', 'liter', 'gallon')",
					},
				},
				"required": []string{"value", "from_unit", "to_unit"},
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"value", "from_unit", "to_unit"},
		},
	}
}

func (t *UnitConverterTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	value, ok := input["value"].(float64)
	if !ok {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'value' parameter must be a number",
			"Please provide a numeric value to convert.",
			map[string]any{
				"skill": "convert_units",
				"field": "value",
			},
		))
	}

	fromUnit, ok := input["from_unit"].(string)
	if !ok || fromUnit == "" {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'from_unit' parameter is required",
			"Please specify the source unit (e.g., 'm', 'km', 'ft', 'kg', 'lb', 'celsius', 'fahrenheit').",
			map[string]any{
				"skill": "convert_units",
				"field": "from_unit",
			},
		))
	}

	toUnit, ok := input["to_unit"].(string)
	if !ok || toUnit == "" {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'to_unit' parameter is required",
			"Please specify the target unit (e.g., 'm', 'km', 'ft', 'kg', 'lb', 'celsius', 'fahrenheit').",
			map[string]any{
				"skill": "convert_units",
				"field": "to_unit",
			},
		))
	}

	fromUnit = strings.ToLower(fromUnit)
	toUnit = strings.ToLower(toUnit)

	lengthConversions := map[string]float64{
		"m": 1.0, "km": 1000.0, "cm": 0.01, "mm": 0.001,
		"ft": 0.3048, "in": 0.0254, "yd": 0.9144, "mi": 1609.34,
	}

	weightConversions := map[string]float64{
		"kg": 1.0, "g": 0.001, "mg": 0.000001, "lb": 0.453592, "oz": 0.0283495,
	}

	volumeConversions := map[string]float64{
		"l": 1.0, "liter": 1.0, "ml": 0.001, "gallon": 3.78541,
		"quart": 0.946353, "pint": 0.473176, "cup": 0.236588, "fl oz": 0.0295735,
	}

	var result float64
	var category string

	if fromVal, fromOk := lengthConversions[fromUnit]; fromOk {
		if toVal, toOk := lengthConversions[toUnit]; toOk {
			result = (value * fromVal) / toVal
			category = "length"
		} else {
			return resultFromMap(formatErrorResponse(
				"validation_error",
				fmt.Sprintf("Invalid target unit '%s' for length conversion", toUnit),
				"Valid length units: m, km, cm, mm, ft, in, yd, mi",
				map[string]any{
					"skill": "convert_units", "field": "to_unit", "provided": toUnit, "category": "length",
				},
			))
		}
	} else if fromVal, fromOk := weightConversions[fromUnit]; fromOk {
		if toVal, toOk := weightConversions[toUnit]; toOk {
			result = (value * fromVal) / toVal
			category = "weight"
		} else {
			return resultFromMap(formatErrorResponse(
				"validation_error",
				fmt.Sprintf("Invalid target unit '%s' for weight conversion", toUnit),
				"Valid weight units: kg, g, mg, lb, oz",
				map[string]any{
					"skill": "convert_units", "field": "to_unit", "provided": toUnit, "category": "weight",
				},
			))
		}
	} else if fromVal, fromOk := volumeConversions[fromUnit]; fromOk {
		if toVal, toOk := volumeConversions[toUnit]; toOk {
			result = (value * fromVal) / toVal
			category = "volume"
		} else {
			return resultFromMap(formatErrorResponse(
				"validation_error",
				fmt.Sprintf("Invalid target unit '%s' for volume conversion", toUnit),
				"Valid volume units: l, liter, ml, gallon, quart, pint, cup, fl oz",
				map[string]any{
					"skill": "convert_units", "field": "to_unit", "provided": toUnit, "category": "volume",
				},
			))
		}
	} else if strings.Contains(fromUnit, "celsius") || strings.Contains(fromUnit, "fahrenheit") {
		category = "temperature"
		var celsius float64

		if strings.Contains(fromUnit, "fahrenheit") {
			celsius = (value - 32) * 5 / 9
		} else {
			celsius = value
		}

		if strings.Contains(toUnit, "fahrenheit") {
			result = celsius*9/5 + 32
		} else {
			result = celsius
		}
	} else {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Unsupported unit conversion from '%s' to '%s'", fromUnit, toUnit),
			"Please ensure both units are of the same type (length, weight, temperature, or volume).",
			map[string]any{
				"skill": "convert_units", "from_unit": fromUnit, "to_unit": toUnit,
			},
		))
	}

	return resultFromMap(map[string]any{
		"value":      result,
		"from_value": value,
		"from_unit":  fromUnit,
		"to_unit":    toUnit,
		"category":   category,
	})
}
