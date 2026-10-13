package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanFunctionParametersInfersMissingGeminiTypes(t *testing.T) {
	params := map[string]interface{}{
		"properties": map[string]interface{}{
			"projectId": map[string]interface{}{"description": "Project identifier"},
			"paths": map[string]interface{}{
				"items": map[string]interface{}{"type": "string"},
			},
			"options": map[string]interface{}{
				"properties": map[string]interface{}{"enabled": map[string]interface{}{"type": "boolean"}},
			},
		},
		"required": []interface{}{"projectId"},
	}

	cleaned, ok := CleanFunctionParameters(params).(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "OBJECT", cleaned["type"])
	properties := cleaned["properties"].(map[string]interface{})
	assert.Equal(t, "STRING", properties["projectId"].(map[string]interface{})["type"])
	assert.Equal(t, "ARRAY", properties["paths"].(map[string]interface{})["type"])
	assert.Equal(t, "OBJECT", properties["options"].(map[string]interface{})["type"])
}

func TestFunctionDeclarationUsesJSONSchemaForAnyOfWithSiblings(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"account": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "null"},
				},
				"description": "Account identifier",
				"const":       "primary",
			},
		},
	}

	declaration, misfits := FunctionDeclaration("pull_requests_checks", "", schema, true)
	assert.Empty(t, misfits)
	assert.NotContains(t, declaration, "parameters")
	jsonSchema, ok := declaration["parametersJsonSchema"].(map[string]any)
	if assert.True(t, ok) {
		properties := jsonSchema["properties"].(map[string]any)
		account := properties["account"].(map[string]any)
		assert.NotContains(t, account, "anyOf")
		assert.Equal(t, "string", account["type"])
		assert.Equal(t, true, account["nullable"])
		assert.Equal(t, "Account identifier", account["description"])
		assert.Equal(t, "primary", account["const"])
	}
}

func TestFunctionDeclarationMovesAnyOfSiblingDescriptionIntoBranches(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"account": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "integer"},
				},
				"description": "Account identifier",
			},
		},
	}

	declaration, misfits := FunctionDeclaration("pull_requests_checks", "", schema, false)
	assert.Empty(t, misfits)
	parameters := declaration["parameters"].(map[string]any)
	properties := parameters["properties"].(map[string]any)
	account := properties["account"].(map[string]any)
	assert.Len(t, account, 1)
	branches := account["anyOf"].([]any)
	require.Len(t, branches, 2)
	assert.Equal(t, "Account identifier", branches[0].(map[string]any)["description"])
	assert.Equal(t, "Account identifier", branches[1].(map[string]any)["description"])
}

func TestCleanFunctionParametersCompactsNullableAnyOfWithSiblings(t *testing.T) {
	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"account": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "null"},
				},
				"description": "Account identifier",
			},
		},
	}

	cleaned, ok := CleanFunctionParameters(params).(map[string]any)
	require.True(t, ok)
	properties := cleaned["properties"].(map[string]any)
	account := properties["account"].(map[string]any)
	assert.Equal(t, "STRING", account["type"])
	assert.Equal(t, true, account["nullable"])
	assert.Equal(t, "Account identifier", account["description"])
	assert.NotContains(t, account, "anyOf")
}
