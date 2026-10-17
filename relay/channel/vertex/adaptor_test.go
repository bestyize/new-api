package vertex

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertOpenAIResponsesRequestToVertexGemini(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-2.5-flash",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-2.5-flash",
		},
	}
	adaptor := &Adaptor{}
	adaptor.Init(info)

	converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, info, dto.OpenAIResponsesRequest{
		Model: "gemini-2.5-flash",
		Input: []byte(`"hello"`),
	})
	require.NoError(t, err)
	request, ok := converted.(*dto.GeminiChatRequest)
	require.True(t, ok)
	require.Len(t, request.Contents, 1)
	assert.Equal(t, "user", request.Contents[0].Role)
	assert.Equal(t, "hello", request.Contents[0].Parts[0].Text)
}

func TestConvertClaudeRequestToVertexGemini(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: "gemini-2.5-flash",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-2.5-flash",
		},
	}
	adaptor := &Adaptor{}
	adaptor.Init(info)

	converted, err := adaptor.ConvertClaudeRequest(nil, info, &dto.ClaudeRequest{
		Model:     "gemini-2.5-flash",
		MaxTokens: func() *uint { v := uint(128); return &v }(),
		Messages: []dto.ClaudeMessage{{
			Role:    "user",
			Content: "hello",
		}},
	})
	require.NoError(t, err)
	request, ok := converted.(*dto.GeminiChatRequest)
	require.True(t, ok)
	require.Len(t, request.Contents, 1)
	assert.Equal(t, "user", request.Contents[0].Role)
	assert.Equal(t, "hello", request.Contents[0].Parts[0].Text)
}
