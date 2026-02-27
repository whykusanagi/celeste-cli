package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoogleConvertSchemaRequiredFromStringSlice(t *testing.T) {
	backend := &GoogleBackend{}

	schema := backend.convertSchemaToGenAI(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]interface{}{
				"type": "string",
			},
		},
		"required": []string{"location"},
	})

	require.NotNil(t, schema)
	assert.Equal(t, []string{"location"}, schema.Required)
}

func TestGoogleConvertSchemaRequiredFromInterfaceSlice(t *testing.T) {
	backend := &GoogleBackend{}

	schema := backend.convertSchemaToGenAI(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]interface{}{
				"type": "string",
			},
		},
		"required": []interface{}{"location"},
	})

	require.NotNil(t, schema)
	assert.Equal(t, []string{"location"}, schema.Required)
}
