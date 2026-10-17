package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/convdiag"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

var geminiOpenAPISchemaAllowedFields = map[string]struct{}{
	"anyOf":            {},
	"default":          {},
	"description":      {},
	"enum":             {},
	"example":          {},
	"format":           {},
	"items":            {},
	"maxItems":         {},
	"maxLength":        {},
	"maxProperties":    {},
	"maximum":          {},
	"minItems":         {},
	"minLength":        {},
	"minProperties":    {},
	"minimum":          {},
	"nullable":         {},
	"pattern":          {},
	"properties":       {},
	"propertyOrdering": {},
	"required":         {},
	"title":            {},
	"type":             {},
}

const geminiFunctionSchemaMaxDepth = 64

func CleanFunctionParameters(params interface{}) interface{} {
	return cleanGeminiFunctionParametersWithDepth(params, 0)
}

func cleanGeminiFunctionParametersWithDepth(params interface{}, depth int) interface{} {
	if params == nil {
		return nil
	}

	if depth >= geminiFunctionSchemaMaxDepth {
		return cleanGeminiFunctionParametersShallow(params)
	}

	switch v := params.(type) {
	case map[string]interface{}:
		cleanedMap := make(map[string]interface{}, len(v))
		for key, val := range v {
			if _, ok := geminiOpenAPISchemaAllowedFields[key]; ok {
				cleanedMap[key] = val
			}
		}

		normalizeGeminiSchemaTypeAndNullable(cleanedMap, depth)
		sanitizeGeminiEnum(cleanedMap)

		if props, ok := cleanedMap["properties"].(map[string]interface{}); ok && props != nil {
			cleanedProps := make(map[string]interface{})
			for propName, propValue := range props {
				cleanedProps[propName] = cleanGeminiFunctionParametersWithDepth(propValue, depth+1)
			}
			cleanedMap["properties"] = cleanedProps
		}

		if items, ok := cleanedMap["items"].(map[string]interface{}); ok && items != nil {
			cleanedMap["items"] = cleanGeminiFunctionParametersWithDepth(items, depth+1)
		}
		if itemsArray, ok := cleanedMap["items"].([]interface{}); ok && len(itemsArray) > 0 {
			cleanedMap["items"] = cleanGeminiFunctionParametersWithDepth(itemsArray[0], depth+1)
		}

		if nested, ok := cleanedMap["anyOf"].([]interface{}); ok && nested != nil {
			cleanedNested := make([]interface{}, len(nested))
			for i, item := range nested {
				cleanedNested[i] = cleanGeminiFunctionParametersWithDepth(item, depth+1)
			}
			cleanedMap["anyOf"] = cleanedNested
		}

		return cleanedMap
	case []interface{}:
		cleanedArray := make([]interface{}, len(v))
		for i, item := range v {
			cleanedArray[i] = cleanGeminiFunctionParametersWithDepth(item, depth+1)
		}
		return cleanedArray
	default:
		return params
	}
}

func cleanGeminiFunctionParametersShallow(params interface{}) interface{} {
	switch v := params.(type) {
	case map[string]interface{}:
		cleanedMap := make(map[string]interface{}, len(v))
		for key, val := range v {
			if _, ok := geminiOpenAPISchemaAllowedFields[key]; ok {
				cleanedMap[key] = val
			}
		}
		normalizeGeminiSchemaTypeAndNullable(cleanedMap, geminiFunctionSchemaMaxDepth)
		sanitizeGeminiEnum(cleanedMap)
		delete(cleanedMap, "properties")
		delete(cleanedMap, "items")
		delete(cleanedMap, "anyOf")
		return cleanedMap
	case []interface{}:
		return []interface{}{}
	default:
		return params
	}
}

// sanitizeGeminiEnum keeps the enum constraint only when every element is a
// string, the only form Gemini's legacy Schema accepts. Any other shape is
// dropped (e.g. boolean or number elements, which valid JSON Schema allows but
// Gemini rejects with a 400). This widens the value domain for that schema, a
// deliberate lossy relaxation: the declared type is preserved, string enums
// pass through untouched.
func sanitizeGeminiEnum(schema map[string]interface{}) {
	raw, exists := schema["enum"]
	if !exists {
		return
	}
	values, ok := raw.([]interface{})
	if !ok {
		delete(schema, "enum")
		return
	}
	for _, value := range values {
		if _, isString := value.(string); !isString {
			delete(schema, "enum")
			return
		}
	}
}

func normalizeGeminiSchemaTypeAndNullable(schema map[string]interface{}, depth int) {
	if rawAnyOf, hasAnyOf := schema["anyOf"]; hasAnyOf {
		if variants, ok := rawAnyOf.([]interface{}); ok {
			if typeName, nullable, compact := compactNullableAnyOf(variants); compact {
				delete(schema, "anyOf")
				schema["type"] = typeName
				if nullable {
					schema["nullable"] = true
				}
			} else {
				// Gemini's typed Schema requires anyOf to be the only member at
				// its level. Keep the union itself and let the caller report any
				// sibling constraints that could not be represented.
				for key := range schema {
					if key != "anyOf" {
						delete(schema, key)
					}
				}
				return
			}
		} else {
			for key := range schema {
				if key != "anyOf" {
					delete(schema, key)
				}
			}
			return
		}
	}

	rawType, ok := schema["type"]
	if !ok || rawType == nil {
		if _, hasProperties := schema["properties"]; hasProperties {
			schema["type"] = "OBJECT"
			return
		}
		if _, hasItems := schema["items"]; hasItems {
			schema["type"] = "ARRAY"
			return
		}
		if enum, ok := schema["enum"].([]interface{}); ok && len(enum) > 0 {
			schema["type"] = geminiSchemaTypeOfValue(enum[0])
			return
		}
		if example, exists := schema["default"]; exists {
			schema["type"] = geminiSchemaTypeOfValue(example)
			return
		}
		if example, exists := schema["example"]; exists {
			schema["type"] = geminiSchemaTypeOfValue(example)
			return
		}
		if depth == 0 {
			schema["type"] = "OBJECT"
		} else {
			// Vertex's typed function Schema requires a concrete type. For a
			// leaf property with no usable type hint, STRING is the least
			// surprising fallback for common tool arguments such as IDs.
			schema["type"] = "STRING"
		}
		return
	}

	normalize := func(t string) (string, bool) {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "object":
			return "OBJECT", false
		case "array":
			return "ARRAY", false
		case "string":
			return "STRING", false
		case "integer":
			return "INTEGER", false
		case "number":
			return "NUMBER", false
		case "boolean":
			return "BOOLEAN", false
		case "null":
			return "", true
		default:
			return t, false
		}
	}

	switch typed := rawType.(type) {
	case string:
		normalized, isNull := normalize(typed)
		if isNull {
			schema["nullable"] = true
			delete(schema, "type")
			return
		}
		schema["type"] = normalized
	case []interface{}:
		nullable := false
		var chosen string
		for _, item := range typed {
			if value, ok := item.(string); ok {
				normalized, isNull := normalize(value)
				if isNull {
					nullable = true
					continue
				}
				if chosen == "" {
					chosen = normalized
				}
			}
		}
		if nullable {
			schema["nullable"] = true
		}
		if chosen != "" {
			schema["type"] = chosen
		} else {
			delete(schema, "type")
		}
	}
}

// compactNullableAnyOf converts the common JSON Schema union
// `anyOf: [{type: T}, {type: null}]` into Gemini's typed `type: T, nullable:
// true` representation. More complex unions remain as an anyOf-only schema.
func compactNullableAnyOf(variants []interface{}) (typeName string, nullable bool, ok bool) {
	if len(variants) == 0 {
		return "", false, false
	}
	for _, variant := range variants {
		object, valid := variant.(map[string]interface{})
		if !valid || len(object) != 1 {
			return "", false, false
		}
		value, valid := object["type"].(string)
		if !valid {
			return "", false, false
		}
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "null":
			if nullable {
				return "", false, false
			}
			nullable = true
		default:
			normalized, isNull := normalizeSchemaType(value)
			if isNull || normalized == "" || (typeName != "" && typeName != normalized) {
				return "", false, false
			}
			typeName = normalized
		}
	}
	if typeName == "" {
		return "", false, false
	}
	return typeName, nullable, true
}

func normalizeSchemaType(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "object":
		return "OBJECT", false
	case "array":
		return "ARRAY", false
	case "string":
		return "STRING", false
	case "integer":
		return "INTEGER", false
	case "number":
		return "NUMBER", false
	case "boolean":
		return "BOOLEAN", false
	case "null":
		return "", true
	default:
		return "", false
	}
}

func geminiSchemaTypeOfValue(value interface{}) string {
	switch value.(type) {
	case nil:
		return "STRING"
	case string:
		return "STRING"
	case bool:
		return "BOOLEAN"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "INTEGER"
	case float32, float64:
		return "NUMBER"
	case []interface{}:
		return "ARRAY"
	case map[string]interface{}:
		return "OBJECT"
	default:
		return "STRING"
	}
}

// FunctionParameters returns the Gemini function declaration field that
// carries schema, and its value. A schema that fits Gemini's typed Schema is
// cleaned as before, and still left out when its properties are empty. AnyOf
// siblings are normalized before either the typed or JSON Schema path so they
// cannot produce a malformed Gemini Schema object.
func FunctionParameters(schema any, jsonSchema bool) (field string, value any, misfits []string) {
	normalizedSchema, _, normalizationLosses := normalizeGeminiJSONSchemaAnyOfSiblings(schema)
	misfits = append(schemaMisfits(normalizedSchema), normalizationLosses...)
	object, _ := normalizedSchema.(map[string]any)
	properties, hasProperties := object["properties"].(map[string]any)
	emptyObject := hasProperties && len(properties) == 0
	if emptyObject {
		for key, item := range object {
			switch key {
			case "type", "properties":
				continue
			case "required":
				if required, _ := item.([]any); len(required) == 0 {
					continue
				}
			}
			if _, allowed := geminiOpenAPISchemaAllowedFields[key]; allowed {
				misfits = append(misfits, "constraints on an empty object")
				break
			}
		}
	}
	if len(misfits) > 0 && jsonSchema {
		return "parametersJsonSchema", normalizedSchema, normalizationLosses
	}
	if emptyObject {
		return "parameters", nil, misfits
	}
	return "parameters", CleanFunctionParameters(normalizedSchema), misfits
}

// normalizeGeminiJSONSchemaAnyOfSiblings makes anyOf the only keyword at its
// schema level, as required by Gemini's typed Schema. Simple nullable unions
// are represented as type plus nullable; other union annotations and
// constraints are moved into each branch when that is unambiguous.
func normalizeGeminiJSONSchemaAnyOfSiblings(schema any) (normalized any, changed bool, losses []string) {
	lost := make(map[string]struct{})
	var normalizeAtDepth func(any, string, int) any
	normalizeAtDepth = func(value any, parentKey string, depth int) any {
		switch typed := value.(type) {
		case map[string]any:
			if depth >= geminiFunctionSchemaMaxDepth {
				for key := range typed {
					if isGeminiJSONSchemaChild(key) {
						lost[fmt.Sprintf("nesting deeper than %d levels", geminiFunctionSchemaMaxDepth)] = struct{}{}
						changed = true
						return map[string]any{"type": "string"}
					}
				}
				return value
			}
			result := make(map[string]any, len(typed))
			for key, item := range typed {
				result[key] = item
			}

			if rawAnyOf, hasAnyOf := result["anyOf"]; hasAnyOf {
				variants, isArray := rawAnyOf.([]any)
				if isArray {
					keepAnyOf := true
					if typeName, nullable, compact := compactNullableAnyOf(variants); compact {
						if existingType, exists := result["type"]; !exists || strings.EqualFold(kitutil.Interface2String(existingType), strings.ToLower(typeName)) {
							delete(result, "anyOf")
							result["type"] = strings.ToLower(typeName)
							if nullable {
								result["nullable"] = true
							}
							changed = true
							keepAnyOf = false
						} else {
							variants = moveAnyOfSiblingsToVariants(result, variants, lost)
							changed = true
						}
					} else if len(result) > 1 {
						variants = moveAnyOfSiblingsToVariants(result, variants, lost)
						changed = true
					}
					if keepAnyOf {
						result["anyOf"] = variants
					}
				} else if len(result) > 1 {
					for key := range result {
						if key != "anyOf" {
							lost[key] = struct{}{}
							delete(result, key)
						}
					}
					changed = true
				}
			}

			for key, item := range result {
				if isGeminiJSONSchemaMapChild(parentKey) {
					result[key] = normalizeAtDepth(item, "", depth+1)
				} else if isGeminiJSONSchemaChild(key) {
					result[key] = normalizeAtDepth(item, key, depth+1)
				}
			}
			return result
		case []any:
			if !isGeminiJSONSchemaArrayChild(parentKey) {
				return value
			}
			result := make([]any, len(typed))
			for index, item := range typed {
				result[index] = normalizeAtDepth(item, parentKey, depth+1)
			}
			return result
		default:
			return value
		}
	}

	normalized = normalizeAtDepth(schema, "", 0)
	losses = make([]string, 0, len(lost))
	for keyword := range lost {
		losses = append(losses, keyword)
	}
	slices.Sort(losses)
	return normalized, changed, losses
}

func moveAnyOfSiblingsToVariants(schema map[string]any, variants []any, losses map[string]struct{}) []any {
	siblings := make(map[string]any, len(schema)-1)
	for key, value := range schema {
		if key != "anyOf" {
			siblings[key] = value
			delete(schema, key)
		}
	}
	result := make([]any, len(variants))
	for index, variant := range variants {
		branch, ok := variant.(map[string]any)
		if !ok {
			for key := range siblings {
				losses[key] = struct{}{}
			}
			result[index] = variant
			continue
		}
		copyBranch := make(map[string]any, len(branch)+len(siblings))
		for key, value := range branch {
			copyBranch[key] = value
		}
		for key, value := range siblings {
			if existing, exists := copyBranch[key]; exists {
				if reflect.DeepEqual(existing, value) {
					continue
				}
				if key == "description" {
					existingText, existingOK := existing.(string)
					valueText, valueOK := value.(string)
					if existingOK && valueOK {
						copyBranch[key] = strings.TrimSpace(valueText + "\n" + existingText)
						continue
					}
				}
				losses[key] = struct{}{}
				continue
			}
			copyBranch[key] = value
		}
		result[index] = copyBranch
	}
	return result
}

func isGeminiJSONSchemaChild(key string) bool {
	switch key {
	case "properties", "patternProperties", "items", "additionalProperties", "unevaluatedProperties", "anyOf", "oneOf", "allOf", "not", "contains", "propertyNames", "definitions", "$defs", "dependentSchemas", "if", "then", "else":
		return true
	default:
		return false
	}
}

func isGeminiJSONSchemaMapChild(key string) bool {
	switch key {
	case "properties", "patternProperties", "definitions", "$defs", "dependentSchemas":
		return true
	default:
		return false
	}
}

func isGeminiJSONSchemaArrayChild(key string) bool {
	switch key {
	case "items", "anyOf", "oneOf", "allOf":
		return true
	default:
		return false
	}
}

// FunctionDeclaration builds the JSON object carried by Gemini's
// functionDeclarations field. Vertex accepts parametersJsonSchema, while the
// typed parameters field rejects JSON Schema constructs such as anyOf with
// sibling keywords.
func FunctionDeclaration(name, description string, schema any, jsonSchema bool) (map[string]any, []string) {
	declaration := map[string]any{"name": name}
	if description != "" {
		declaration["description"] = description
	}
	if schema != nil {
		field, value, misfits := FunctionParameters(schema, jsonSchema)
		if value != nil {
			declaration[field] = value
		}
		return declaration, misfits
	}
	return declaration, nil
}

// ApplyResponseSchema sets the structured output schema from the json_schema
// object of an OpenAI response format. A schema that fits Gemini's typed
// Schema keeps the responseSchema cleanup. Any other schema is sent unchanged
// as responseJsonSchema when jsonSchema is true; otherwise it is cleaned
// anyway and a diagnostic at path names what responseSchema cannot carry.
func ApplyResponseSchema(ctx context.Context, config *dto.GeminiChatGenerationConfig, rawFormat json.RawMessage, path string, jsonSchema bool) {
	var format dto.FormatJsonSchema
	if err := kitutil.Unmarshal(rawFormat, &format); err != nil {
		return
	}
	normalizedSchema, changed, normalizationLosses := normalizeGeminiJSONSchemaAnyOfSiblings(format.Schema)
	misfits := schemaMisfits(normalizedSchema)
	if len(misfits) > 0 && jsonSchema {
		if changed {
			if encoded, err := kitutil.Marshal(normalizedSchema); err == nil {
				config.ResponseJsonSchema = encoded
			}
		} else {
			// Preserve the client's schema bytes when no normalization is needed.
			var raw struct {
				Schema json.RawMessage `json:"schema"`
			}
			if err := kitutil.Unmarshal(rawFormat, &raw); err == nil {
				config.ResponseJsonSchema = raw.Schema
			}
		}
		if len(config.ResponseJsonSchema) > 0 {
			if len(normalizationLosses) > 0 {
				convdiag.Add(ctx, types.ConversionDiagnostic{
					Code:     "gemini_schema_keyword_dropped",
					Path:     path,
					Message:  fmt.Sprintf("Gemini schema cannot represent %s alongside anyOf", strings.Join(normalizationLosses, ", ")),
					Severity: types.ConversionDiagnosticWarning,
				})
			}
			return
		}
	}
	config.ResponseSchema = RemoveAdditionalProperties(normalizedSchema, 0)
	if len(misfits) > 0 || len(normalizationLosses) > 0 {
		dropped := append(misfits, normalizationLosses...)
		slices.Sort(dropped)
		dropped = slices.Compact(dropped)
		convdiag.Add(ctx, types.ConversionDiagnostic{
			Code:     "gemini_schema_keyword_dropped",
			Path:     path,
			Message:  fmt.Sprintf("Gemini response schema cannot carry %s", strings.Join(dropped, ", ")),
			Severity: types.ConversionDiagnosticWarning,
		})
	}
}

// schemaMisfits lists what Gemini's typed Schema cannot hold of a JSON Schema
// as written: keywords outside its fields, type arrays, non-string enum
// values, tuple items, boolean schemas, and nesting deeper than
// CleanFunctionParameters keeps. additionalProperties and $schema are not
// misfits: agent tools and OpenAI strict schemas carry them on nearly every
// object, and dropping them as before keeps those requests unchanged for
// upstreams that do not pass the JSON Schema fields through.
func schemaMisfits(schema any) []string {
	found := make(map[string]struct{})
	collectSchemaMisfits(schema, 0, found)
	misfits := make([]string, 0, len(found))
	for misfit := range found {
		misfits = append(misfits, misfit)
	}
	slices.Sort(misfits)
	return misfits
}

func collectSchemaMisfits(schema any, depth int, found map[string]struct{}) {
	if depth > geminiFunctionSchemaMaxDepth {
		found[fmt.Sprintf("nesting deeper than %d levels", geminiFunctionSchemaMaxDepth)] = struct{}{}
		return
	}
	value, ok := schema.(map[string]any)
	if !ok {
		if _, isBool := schema.(bool); isBool {
			found["boolean schemas"] = struct{}{}
		}
		return
	}
	if _, hasAnyOf := value["anyOf"]; hasAnyOf && len(value) > 1 {
		found["anyOf with sibling keywords"] = struct{}{}
	}
	for key, item := range value {
		if _, allowed := geminiOpenAPISchemaAllowedFields[key]; !allowed {
			if key != "additionalProperties" && key != "$schema" {
				found[key] = struct{}{}
			}
			continue
		}
		switch key {
		case "type":
			if _, isArray := item.([]any); isArray {
				found["type arrays"] = struct{}{}
			}
		case "enum":
			values, _ := item.([]any)
			if slices.ContainsFunc(values, func(value any) bool {
				_, isString := value.(string)
				return !isString
			}) {
				found["non-string enum values"] = struct{}{}
			}
		case "properties":
			properties, _ := item.(map[string]any)
			for _, property := range properties {
				collectSchemaMisfits(property, depth+1, found)
			}
		case "items":
			entries, isTuple := item.([]any)
			if !isTuple {
				collectSchemaMisfits(item, depth+1, found)
				continue
			}
			found["tuple items"] = struct{}{}
			for _, entry := range entries {
				collectSchemaMisfits(entry, depth+1, found)
			}
		case "anyOf":
			variants, _ := item.([]any)
			for _, variant := range variants {
				collectSchemaMisfits(variant, depth+1, found)
			}
		}
	}
}

func RemoveAdditionalProperties(schema interface{}, depth int) interface{} {
	if depth >= 5 {
		return schema
	}

	value, ok := schema.(map[string]interface{})
	if !ok || len(value) == 0 {
		return schema
	}
	delete(value, "title")
	delete(value, "$schema")
	if typeVal, exists := value["type"]; !exists || (typeVal != "object" && typeVal != "array") {
		return schema
	}
	switch value["type"] {
	case "object":
		delete(value, "additionalProperties")
		if properties, ok := value["properties"].(map[string]interface{}); ok {
			for key, nested := range properties {
				properties[key] = RemoveAdditionalProperties(nested, depth+1)
			}
		}
		for _, field := range []string{"allOf", "anyOf", "oneOf"} {
			if nested, ok := value[field].([]interface{}); ok {
				for i, item := range nested {
					nested[i] = RemoveAdditionalProperties(item, depth+1)
				}
			}
		}
	case "array":
		if items, ok := value["items"].(map[string]interface{}); ok {
			value["items"] = RemoveAdditionalProperties(items, depth+1)
		}
	}

	return value
}

func OpenAIToolChoiceToConfig(toolChoice any) *dto.ToolConfig {
	if toolChoice == nil {
		return nil
	}

	if toolChoiceStr, ok := toolChoice.(string); ok {
		config := &dto.ToolConfig{
			FunctionCallingConfig: &dto.FunctionCallingConfig{},
		}
		switch toolChoiceStr {
		case "auto":
			config.FunctionCallingConfig.Mode = "AUTO"
		case "none":
			config.FunctionCallingConfig.Mode = "NONE"
		case "required":
			config.FunctionCallingConfig.Mode = "ANY"
		default:
			config.FunctionCallingConfig.Mode = "AUTO"
		}
		return config
	}

	if toolChoiceMap, ok := toolChoice.(map[string]interface{}); ok {
		if toolChoiceMap["type"] == "function" {
			config := &dto.ToolConfig{
				FunctionCallingConfig: &dto.FunctionCallingConfig{
					Mode: "ANY",
				},
			}
			if function, ok := toolChoiceMap["function"].(map[string]interface{}); ok {
				if name, ok := function["name"].(string); ok && name != "" {
					config.FunctionCallingConfig.AllowedFunctionNames = []string{name}
				}
			}
			return config
		}
		return nil
	}

	return nil
}

// geminiSchemaInt64Fields are Gemini Schema bounds typed int64, which the
// proto JSON encoding may send as strings.
var geminiSchemaInt64Fields = map[string]struct{}{
	"maxItems":      {},
	"minItems":      {},
	"maxLength":     {},
	"minLength":     {},
	"maxProperties": {},
	"minProperties": {},
}

// OpenAPISchemaToJSONSchema rewrites a schema in Gemini's OpenAPI subset as
// JSON Schema: type names are lowercased, nullable becomes a "null" type,
// example becomes examples, and int64 bounds sent as strings become numbers.
// It also returns the Gemini-only keywords JSON Schema cannot express, which
// are dropped (propertyOrdering).
func OpenAPISchemaToJSONSchema(schema any) (any, []string) {
	dropped := make(map[string]struct{})
	converted := openAPISchemaToJSONSchema(schema, 0, dropped)
	keywords := make([]string, 0, len(dropped))
	for keyword := range dropped {
		keywords = append(keywords, keyword)
	}
	slices.Sort(keywords)
	return converted, keywords
}

func openAPISchemaToJSONSchema(schema any, depth int, dropped map[string]struct{}) any {
	value, ok := schema.(map[string]any)
	if !ok || depth >= geminiFunctionSchemaMaxDepth {
		return schema
	}
	converted := make(map[string]any, len(value))
	nullable := false
	for key, item := range value {
		switch key {
		case "type":
			typeName, isString := item.(string)
			if !isString {
				converted[key] = item
				continue
			}
			typeName = strings.ToLower(strings.TrimSpace(typeName))
			if typeName != "" && typeName != "type_unspecified" {
				converted[key] = typeName
			}
		case "nullable":
			nullable, _ = item.(bool)
		case "propertyOrdering":
			dropped[key] = struct{}{}
		case "example":
			if _, exists := value["examples"]; exists {
				dropped[key] = struct{}{}
				continue
			}
			converted["examples"] = []any{item}
		case "properties":
			properties, isMap := item.(map[string]any)
			if !isMap {
				converted[key] = item
				continue
			}
			convertedProperties := make(map[string]any, len(properties))
			for name, property := range properties {
				convertedProperties[name] = openAPISchemaToJSONSchema(property, depth+1, dropped)
			}
			converted[key] = convertedProperties
		case "items":
			converted[key] = openAPISchemaToJSONSchema(item, depth+1, dropped)
		case "anyOf":
			variants, isArray := item.([]any)
			if !isArray {
				converted[key] = item
				continue
			}
			convertedVariants := make([]any, len(variants))
			for index, variant := range variants {
				convertedVariants[index] = openAPISchemaToJSONSchema(variant, depth+1, dropped)
			}
			converted[key] = convertedVariants
		default:
			if _, isInt64 := geminiSchemaInt64Fields[key]; isInt64 {
				if text, isString := item.(string); isString {
					if number, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64); err == nil {
						converted[key] = number
						continue
					}
				}
			}
			converted[key] = item
		}
	}
	if !nullable {
		return converted
	}
	switch typed := converted["type"].(type) {
	case string:
		converted["type"] = []any{typed, "null"}
	default:
		if variants, isArray := converted["anyOf"].([]any); isArray {
			converted["anyOf"] = append(variants, map[string]any{"type": "null"})
		}
	}
	if values, isArray := converted["enum"].([]any); isArray && !slices.Contains(values, nil) {
		converted["enum"] = append(values, nil)
	}
	return converted
}
