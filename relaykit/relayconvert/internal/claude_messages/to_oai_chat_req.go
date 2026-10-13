package claudemessages

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/convdiag"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
)

const (
	webSearchMaxUsesLow    = 1
	webSearchMaxUsesMedium = 5
	webSearchMaxUsesHigh   = 10
)

type openRouterRequestReasoning struct {
	Enabled   *bool  `json:"enabled,omitempty"`
	Effort    string `json:"effort,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	Exclude   bool   `json:"exclude,omitempty"`
}

func ClaudeMessagesRequestToOpenAIChat(ctx context.Context, claudeRequest dto.ClaudeRequest, info convmeta.Meta) (*dto.GeneralOpenAIRequest, error) {
	openAIRequest := dto.GeneralOpenAIRequest{
		Model:       claudeRequest.Model,
		Temperature: claudeRequest.Temperature,
	}
	if claudeRequest.MaxTokens != nil {
		openAIRequest.MaxTokens = kitutil.GetPointer(*claudeRequest.MaxTokens)
	} else if claudeRequest.MaxTokensToSample != nil {
		openAIRequest.MaxTokens = kitutil.GetPointer(*claudeRequest.MaxTokensToSample)
	}
	if claudeRequest.TopP != nil {
		openAIRequest.TopP = kitutil.GetPointer(*claudeRequest.TopP)
	}
	if claudeRequest.TopK != nil {
		openAIRequest.TopK = kitutil.GetPointer(*claudeRequest.TopK)
	}
	if claudeRequest.Stream != nil {
		openAIRequest.Stream = kitutil.GetPointer(*claudeRequest.Stream)
	}
	reasoningIntent, effectiveEffort, err := claudeRequestReasoningIntent(ctx, &claudeRequest, info)
	if err != nil {
		return nil, reasoning.AsClientError(err)
	}

	isOpenRouter := convmeta.OptionsOf(info).OpenRouterDialect
	if isOpenRouter {
		if effort := claudeRequest.GetEfforts(); effort != "" {
			effortBytes, _ := kitutil.Marshal(effort)
			openAIRequest.Verbosity = effortBytes
		}
		if !reasoningIntent.IsEmpty() {
			var reasoningConfig openRouterRequestReasoning
			disabled := reasoningIntent.Mode == reasoning.ModeDisabled || reasoningIntent.Effort == reasoning.EffortNone
			enabled := !disabled
			reasoningConfig.Enabled = &enabled
			if enabled && reasoningIntent.BudgetTokens != nil && reasoningIntent.Mode != reasoning.ModeAdaptive {
				reasoningConfig = openRouterRequestReasoning{
					Enabled:   &enabled,
					MaxTokens: *reasoningIntent.BudgetTokens,
				}
			} else if enabled {
				reasoningConfig.Effort = string(reasoning.EffectiveEffort(reasoningIntent))
			}
			if reasoningIntent.IncludeThoughts != nil {
				reasoningConfig.Exclude = !*reasoningIntent.IncludeThoughts
			}
			reasoningJSON, err := kitutil.Marshal(reasoningConfig)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal reasoning: %w", err)
			}
			openAIRequest.Reasoning = reasoningJSON
		}
	} else {
		if err := reasoning.ApplyToOpenAIChat(&openAIRequest, reasoningIntent); err != nil {
			return nil, reasoning.AsClientError(err)
		}
		if info != nil {
			// Keep the outgoing -thinking suffix so a cascaded downstream
			// new-api can recover reasoning intent from the model name. This
			// is an emission-side policy, not converter-side suffix parsing.
			thinkingSuffix := "-thinking"
			if strings.HasSuffix(info.GetOriginModelName(), thinkingSuffix) &&
				!strings.HasSuffix(openAIRequest.Model, thinkingSuffix) {
				openAIRequest.Model = openAIRequest.Model + thinkingSuffix
			}
		}
	}
	if info != nil && effectiveEffort != "" {
		info.SetReasoningEffort(string(effectiveEffort))
	}

	if len(claudeRequest.StopSequences) == 1 {
		openAIRequest.Stop = claudeRequest.StopSequences[0]
	} else if len(claudeRequest.StopSequences) > 1 {
		openAIRequest.Stop = claudeRequest.StopSequences
	}

	tools, _ := kitutil.Any2Type[[]dto.Tool](claudeRequest.Tools)
	openAITools := make([]dto.ToolCallRequest, 0)
	for _, claudeTool := range tools {
		openAITool := dto.ToolCallRequest{
			Type: "function",
			Function: dto.FunctionRequest{
				Name:        claudeTool.Name,
				Description: claudeTool.Description,
				Parameters:  claudeTool.InputSchema,
			},
		}
		openAITools = append(openAITools, openAITool)
	}
	openAIRequest.Tools = openAITools
	outputFormat, err := claudeStructuredOutputFormat(claudeRequest.OutputConfig, claudeRequest.OutputFormat)
	if err != nil {
		return nil, err
	}
	if len(outputFormat) > 0 {
		chatSchema, err := claudeStructuredOutputChatSchema(outputFormat)
		if err != nil {
			return nil, err
		}
		openAIRequest.ResponseFormat = &dto.ResponseFormat{
			Type:       "json_schema",
			JsonSchema: chatSchema,
		}
	}
	toolChoice, disableParallelToolUse, err := claudeToolChoiceToChat(claudeRequest.ToolChoice)
	if err != nil {
		return nil, err
	}
	openAIRequest.ToolChoice = toolChoice
	if disableParallelToolUse {
		convdiag.Add(ctx, types.ConversionDiagnostic{
			Code:     "unsupported_parallel_tool_limit",
			Path:     "tool_choice.disable_parallel_tool_use",
			Message:  "Gemini function calling has no setting to prohibit multiple function calls in one turn",
			Severity: types.ConversionDiagnosticWarning,
		})
	}

	openAIMessages := make([]dto.Message, 0)
	if claudeRequest.System != nil {
		if claudeRequest.IsStringSystem() && claudeRequest.GetStringSystem() != "" {
			openAIMessage := dto.Message{
				Role: "system",
			}
			openAIMessage.SetStringContent(claudeRequest.GetStringSystem())
			openAIMessages = append(openAIMessages, openAIMessage)
		} else {
			systems := claudeRequest.ParseSystem()
			if len(systems) > 0 {
				openAIMessage := dto.Message{
					Role: "system",
				}
				isOpenRouterClaude := isOpenRouter && strings.HasPrefix(convmeta.UpstreamModelName(info), "anthropic/claude")
				if isOpenRouterClaude {
					systemMediaMessages := make([]dto.MediaContent, 0, len(systems))
					for _, system := range systems {
						message := dto.MediaContent{
							Type:         "text",
							Text:         system.GetText(),
							CacheControl: system.CacheControl,
						}
						systemMediaMessages = append(systemMediaMessages, message)
					}
					openAIMessage.SetMediaContent(systemMediaMessages)
				} else {
					var systemStr strings.Builder
					for _, system := range systems {
						if system.Text != nil {
							systemStr.WriteString(*system.Text)
						}
					}
					openAIMessage.SetStringContent(systemStr.String())
				}
				openAIMessages = append(openAIMessages, openAIMessage)
			}
		}
	}

	type unnamedToolResult struct {
		index int
		id    string
	}
	toolNames := make(map[string]string)
	var unnamedToolResults []unnamedToolResult
	for messageIndex, claudeMessage := range claudeRequest.Messages {
		openAIMessage := dto.Message{
			Role: claudeMessage.Role,
		}
		if claudeMessage.IsStringContent() {
			openAIMessage.SetStringContent(claudeMessage.GetStringContent())
		} else {
			content, err := claudeMessage.ParseContent()
			if err != nil {
				return nil, err
			}
			var toolCalls []dto.ToolCallRequest
			mediaMessages := make([]dto.MediaContent, 0, len(content))

			for blockIndex, mediaMsg := range content {
				if _, exists := toolNames[mediaMsg.Id]; !exists {
					toolNames[mediaMsg.Id] = mediaMsg.Name
				}
				switch mediaMsg.Type {
				case "text", "input_text":
					message := dto.MediaContent{
						Type:         "text",
						Text:         mediaMsg.GetText(),
						CacheControl: mediaMsg.CacheControl,
					}
					mediaMessages = append(mediaMessages, message)
				case "image":
					url := claudeImageSourceURL(mediaMsg.Source)
					if url == "" {
						sourceType := ""
						fileID := ""
						if mediaMsg.Source != nil {
							sourceType = mediaMsg.Source.Type
							fileID = mediaMsg.Source.FileID
						}
						if sourceType == "file" {
							return nil, fmt.Errorf("Claude file-backed image %q cannot be resolved for Gemini; send the image as base64 or a URL", fileID)
						}
						return nil, fmt.Errorf("Claude image at messages[%d].content[%d] has unsupported source type %q or no image data", messageIndex, blockIndex, sourceType)
					}
					mediaMessages = append(mediaMessages, dto.MediaContent{
						Type:     "image_url",
						ImageUrl: &dto.MessageImageUrl{Url: url},
					})
				case "document":
					if mediaMsg.Source != nil && mediaMsg.Source.Type == "text" {
						// A plain-text document keeps its content as a Chat text part.
						mediaMessages = append(mediaMessages, dto.MediaContent{Type: "text", Text: kitutil.Interface2String(mediaMsg.Source.Data)})
						continue
					}
					if mediaMsg.Source == nil || mediaMsg.Source.Type != "base64" || mediaMsg.Source.MediaType != "application/pdf" {
						sourceType, mediaType := "", ""
						fileID := ""
						if mediaMsg.Source != nil {
							sourceType, mediaType = mediaMsg.Source.Type, mediaMsg.Source.MediaType
							fileID = mediaMsg.Source.FileID
						}
						if sourceType == "file" {
							return nil, fmt.Errorf("Claude file-backed document %q cannot be resolved for Gemini; send document content as base64 or text", fileID)
						}
						return nil, fmt.Errorf("Claude document at messages[%d].content[%d] has unsupported source type %q and media type %q", messageIndex, blockIndex, sourceType, mediaType)
					}
					mediaMessages = append(mediaMessages, dto.MediaContent{
						Type: dto.ContentTypeFile,
						File: &dto.MessageFile{
							FileName: "document.pdf",
							FileData: "data:application/pdf;base64," + kitutil.Interface2String(mediaMsg.Source.Data),
						},
					})
				case "tool_use":
					toolCall := dto.ToolCallRequest{
						ID:   mediaMsg.Id,
						Type: "function",
						Function: dto.FunctionRequest{
							Name:      mediaMsg.Name,
							Arguments: requestToJSONString(mediaMsg.Input),
						},
					}
					toolCalls = append(toolCalls, toolCall)
				case "tool_result":
					toolName := mediaMsg.Name
					if toolName == "" {
						unnamedToolResults = append(unnamedToolResults, unnamedToolResult{index: len(openAIMessages), id: mediaMsg.ToolUseId})
					}
					oaiToolMessage := dto.Message{
						Role:       "tool",
						Name:       &toolName,
						ToolCallId: mediaMsg.ToolUseId,
					}
					if mediaMsg.IsStringContent() {
						oaiToolMessage.SetStringContent(mediaMsg.GetStringContent())
					} else {
						content, media, err := claudeToolResultToChat(mediaMsg.ParseMediaContent())
						if err != nil {
							return nil, err
						}
						oaiToolMessage.SetStringContent(content)
						// A Chat tool message only carries text. Images from the tool result
						// join the user message this Claude message becomes, which lands right
						// after the tool batch and keeps the tool messages contiguous.
						mediaMessages = append(mediaMessages, media...)
					}
					openAIMessages = append(openAIMessages, oaiToolMessage)
				case "thinking":
					convdiag.Add(ctx, types.ConversionDiagnostic{
						Code:     "unsupported_thinking_history",
						Path:     fmt.Sprintf("messages[%d].content[%d]", messageIndex, blockIndex),
						Message:  "Claude thinking history is provider-specific and was omitted from Gemini request history",
						Severity: types.ConversionDiagnosticWarning,
						To:       types.RelayFormatGemini,
					})
				case "redacted_thinking":
					convdiag.Add(ctx, types.ConversionDiagnostic{
						Code:     "unsupported_redacted_thinking",
						Path:     fmt.Sprintf("messages[%d].content[%d]", messageIndex, blockIndex),
						Message:  "Claude redacted thinking cannot be represented in Gemini request history",
						Severity: types.ConversionDiagnosticWarning,
						To:       types.RelayFormatGemini,
					})
				default:
					return nil, fmt.Errorf("unsupported Claude message block type %q at messages[%d].content[%d]", mediaMsg.Type, messageIndex, blockIndex)
				}
			}

			if len(toolCalls) > 0 {
				openAIMessage.SetToolCalls(toolCalls)
			}
			if len(mediaMessages) > 0 {
				openAIMessage.SetMediaContent(mediaMessages)
			}
		}
		if len(openAIMessage.ParseContent()) > 0 || len(openAIMessage.ToolCalls) > 0 {
			openAIMessages = append(openAIMessages, openAIMessage)
		}
	}
	for _, result := range unnamedToolResults {
		*openAIMessages[result.index].Name = toolNames[result.id]
	}

	openAIRequest.Messages = openAIMessages
	return &openAIRequest, nil
}

func claudeToolChoiceToChat(value any) (any, bool, error) {
	if value == nil {
		return nil, false, nil
	}
	choice, err := kitutil.Any2Type[dto.ClaudeToolChoice](value)
	if err != nil {
		return nil, false, fmt.Errorf("invalid Claude tool_choice: %w", err)
	}
	switch choice.Type {
	case "", "auto":
		return "auto", choice.DisableParallelToolUse, nil
	case "any":
		return "required", choice.DisableParallelToolUse, nil
	case "none":
		return "none", false, nil
	case "tool":
		if strings.TrimSpace(choice.Name) == "" {
			return nil, false, fmt.Errorf("Claude tool_choice.type=tool requires a tool name")
		}
		return map[string]any{
			"type":     "function",
			"function": map[string]any{"name": choice.Name},
		}, choice.DisableParallelToolUse, nil
	default:
		return nil, false, fmt.Errorf("unsupported Claude tool_choice type %q", choice.Type)
	}
}

func claudeStructuredOutputFormat(outputConfig, legacyFormat json.RawMessage) (json.RawMessage, error) {
	var format json.RawMessage
	if len(outputConfig) > 0 {
		var config struct {
			Format json.RawMessage `json:"format"`
		}
		if err := kitutil.Unmarshal(outputConfig, &config); err != nil {
			return nil, fmt.Errorf("invalid Claude output_config: %w", err)
		}
		format = config.Format
	}
	if len(format) == 0 {
		format = legacyFormat
	}
	if len(format) == 0 || kitutil.GetJsonType(format) == "null" {
		return nil, nil
	}
	var parsed struct {
		Type   string          `json:"type"`
		Name   string          `json:"name"`
		Schema json.RawMessage `json:"schema"`
	}
	if err := kitutil.Unmarshal(format, &parsed); err != nil {
		return nil, fmt.Errorf("invalid Claude structured output format: %w", err)
	}
	if parsed.Type != "json_schema" {
		return nil, fmt.Errorf("unsupported Claude output format type %q", parsed.Type)
	}
	if len(parsed.Schema) == 0 || kitutil.GetJsonType(parsed.Schema) != "object" {
		return nil, fmt.Errorf("Claude json_schema output format requires an object schema")
	}
	formatObject := make(map[string]json.RawMessage)
	if err := kitutil.Unmarshal(format, &formatObject); err != nil {
		return nil, fmt.Errorf("invalid Claude structured output format: %w", err)
	}
	if strings.TrimSpace(parsed.Name) == "" {
		name, err := kitutil.Marshal("claude_output")
		if err != nil {
			return nil, fmt.Errorf("encode Claude structured output name: %w", err)
		}
		formatObject["name"] = name
	}
	return kitutil.Marshal(formatObject)
}

func claudeStructuredOutputChatSchema(format json.RawMessage) (json.RawMessage, error) {
	var schema map[string]json.RawMessage
	if err := kitutil.Unmarshal(format, &schema); err != nil {
		return nil, fmt.Errorf("invalid Claude structured output format: %w", err)
	}
	delete(schema, "type")
	encoded, err := kitutil.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("encode Claude structured output schema: %w", err)
	}
	return encoded, nil
}

// claudeToolResultToChat maps a structured tool_result content array onto Chat Completions,
// where a tool message may only carry text. Text blocks stay on the tool message and image
// blocks come back in Chat shape for the caller to place on the following user message;
// stringifying them instead would hand base64 image data to the upstream text tokenizer.
// Arrays holding any other block type keep the historical stringified form so nothing is lost.
func claudeToolResultToChat(blocks []dto.ClaudeMediaMessage) (string, []dto.MediaContent, error) {
	texts := make([]string, 0, len(blocks))
	media := make([]dto.MediaContent, 0, len(blocks))
	for _, block := range blocks {
		switch {
		case block.Type == "text" || block.Type == "input_text":
			if text := block.GetText(); text != "" {
				texts = append(texts, text)
			}
		case block.Type == "image":
			url := claudeImageSourceURL(block.Source)
			if url == "" {
				if block.Source != nil && block.Source.Type == "file" {
					return "", nil, fmt.Errorf("Claude file-backed tool-result image %q cannot be resolved for Gemini; send the image as base64 or a URL", block.Source.FileID)
				}
				return "", nil, fmt.Errorf("Claude tool-result image has unsupported source type %q or no image data", claudeSourceType(block.Source))
			}
			media = append(media, dto.MediaContent{Type: "image_url", ImageUrl: &dto.MessageImageUrl{Url: url}})
		case block.Type == "document":
			if block.Source != nil && block.Source.Type == "text" {
				texts = append(texts, kitutil.Interface2String(block.Source.Data))
				continue
			}
			if block.Source != nil && block.Source.Type == "file" {
				return "", nil, fmt.Errorf("Claude file-backed tool-result document %q cannot be resolved for Gemini; send document content as base64 or text", block.Source.FileID)
			}
			if block.Source == nil || block.Source.Type != "base64" || block.Source.MediaType != "application/pdf" {
				return requestToJSONString(blocks), nil, nil
			}
			media = append(media, dto.MediaContent{
				Type: dto.ContentTypeFile,
				File: &dto.MessageFile{
					FileName: "document.pdf",
					FileData: "data:application/pdf;base64," + kitutil.Interface2String(block.Source.Data),
				},
			})
		default:
			return requestToJSONString(blocks), nil, nil
		}
	}
	switch {
	case len(texts) == 0 && len(media) == 0:
		return requestToJSONString(blocks), nil, nil
	case len(texts) == 0:
		// Upstreams reject empty tool content; the images ride on the following user message.
		return "[image]", media, nil
	default:
		return strings.Join(texts, "\n"), media, nil
	}
}

func claudeSourceType(source *dto.ClaudeMessageSource) string {
	if source == nil {
		return ""
	}
	return source.Type
}

func claudeSourceMediaType(source *dto.ClaudeMessageSource) string {
	if source == nil {
		return ""
	}
	return source.MediaType
}

// claudeImageSourceURL is the Chat image_url for a Claude image source: a data
// URL for base64 data or the URL of a url source. Other sources, such as Files
// API references, have no Chat equivalent and yield "".
func claudeImageSourceURL(source *dto.ClaudeMessageSource) string {
	if source == nil || source.Type == "file" {
		return ""
	}
	if source.Url != "" {
		return source.Url
	}
	data := kitutil.Interface2String(source.Data)
	if data == "" {
		return ""
	}
	return fmt.Sprintf("data:%s;base64,%s", source.MediaType, data)
}

func requestToJSONString(v any) string {
	b, err := kitutil.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
