package vertex

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVertexClaudeMessagesToGeminiUsesGenerateContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-3.8-flash",
		IsStream:        true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeVertexAi,
			ChannelBaseUrl:    "https://us-central1-aiplatform.googleapis.com",
			ApiKey:            "test-key",
			UpstreamModelName: "gemini-3.8-flash",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				VertexKeyType: dto.VertexKeyTypeAPIKey,
			},
		},
	}
	adaptor := &Adaptor{}
	adaptor.Init(info)
	require.Equal(t, RequestModeGemini, adaptor.RequestMode)

	maxTokens := uint(32)
	got, err := adaptor.ConvertClaudeRequest(ctx, info, &dto.ClaudeRequest{
		Model:     "gemini-3.8-flash",
		MaxTokens: &maxTokens,
		Messages:  []dto.ClaudeMessage{{Role: "user", Content: "hi"}},
		Tools: []dto.Tool{{
			Name: "pull_requests_checks",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"account": map[string]any{
						"anyOf":       []any{map[string]any{"type": "string"}, map[string]any{"type": "null"}},
						"description": "Account name",
					},
				},
			},
		}},
		ToolChoice:   dto.ClaudeToolChoice{Type: "tool", Name: "pull_requests_checks"},
		OutputConfig: json.RawMessage(`{"format":{"type":"json_schema","name":"result","schema":{"type":"object","properties":{"ok":{"type":"boolean"}}}}}`),
	})
	require.NoError(t, err)
	geminiRequest, ok := got.(*dto.GeminiChatRequest)
	require.True(t, ok, "expected Gemini generateContent request, got %T", got)
	require.Len(t, geminiRequest.Contents, 1)
	require.Len(t, geminiRequest.Contents[0].Parts, 1)
	assert.Equal(t, "hi", geminiRequest.Contents[0].Parts[0].Text)
	assert.Equal(t, dto.FunctionCallingConfigMode("ANY"), geminiRequest.ToolConfig.FunctionCallingConfig.Mode)
	assert.Equal(t, []string{"pull_requests_checks"}, geminiRequest.ToolConfig.FunctionCallingConfig.AllowedFunctionNames)
	assert.Equal(t, "application/json", geminiRequest.GenerationConfig.ResponseMimeType)
	require.NotEmpty(t, geminiRequest.GenerationConfig.ResponseSchema)

	var upstream struct {
		Tools []struct {
			FunctionDeclarations []struct {
				Parameters struct {
					Properties map[string]struct {
						Type        string `json:"type"`
						Nullable    bool   `json:"nullable"`
						Description string `json:"description"`
					} `json:"properties"`
				} `json:"parameters"`
			} `json:"functionDeclarations"`
		} `json:"tools"`
	}
	body, err := common.Marshal(geminiRequest)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(body, &upstream))
	require.Len(t, upstream.Tools, 1)
	require.Len(t, upstream.Tools[0].FunctionDeclarations, 1)
	account := upstream.Tools[0].FunctionDeclarations[0].Parameters.Properties["account"]
	assert.Equal(t, "STRING", account.Type)
	assert.True(t, account.Nullable)
	assert.Equal(t, "Account name", account.Description)
	assert.Equal(t, "gemini-3.8-flash", ctx.GetString("request_model"))

	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.True(t, strings.Contains(url, "/publishers/google/models/gemini-3.8-flash:streamGenerateContent?alt=sse"), url)
	assert.NotContains(t, url, "anthropic")
}
