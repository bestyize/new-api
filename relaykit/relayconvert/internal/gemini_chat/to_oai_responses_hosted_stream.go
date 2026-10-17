package geminichat

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	oaichat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/oai_chat"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// GeminiHostedStreamBridge accumulates grounding metadata until the provider
// stream ends. Gemini commonly reports the search queries after emitting the
// answer text; delaying the synthetic Responses item keeps one complete,
// canonical action instead of emitting partial or duplicate tool calls.
type GeminiHostedStreamBridge struct {
	queries   []string
	images    []geminiStreamImage
	seen      map[string]struct{}
	seenImage map[string]struct{}
}

type geminiStreamImage struct {
	data string
}

func NewGeminiHostedStreamBridge() *GeminiHostedStreamBridge {
	return &GeminiHostedStreamBridge{
		seen:      make(map[string]struct{}),
		seenImage: make(map[string]struct{}),
	}
}

func (b *GeminiHostedStreamBridge) Observe(response *dto.GeminiChatResponse) {
	if b == nil || response == nil {
		return
	}
	if b.seen == nil {
		b.seen = make(map[string]struct{})
	}
	for _, query := range GroundingWebSearchQueries(response) {
		if _, exists := b.seen[query]; exists {
			continue
		}
		b.seen[query] = struct{}{}
		b.queries = append(b.queries, query)
	}
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.InlineData == nil || !strings.HasPrefix(strings.ToLower(part.InlineData.MimeType), "image/") || part.InlineData.Data == "" {
				continue
			}
			key := part.InlineData.MimeType + "\x00" + part.InlineData.Data
			if _, exists := b.seenImage[key]; exists {
				continue
			}
			b.seenImage[key] = struct{}{}
			b.images = append(b.images, geminiStreamImage{
				data: part.InlineData.Data,
			})
		}
	}
}

func (b *GeminiHostedStreamBridge) Finalize(state *oaichat.ChatToResponsesStreamState) ([]oaichat.ChatToResponsesStreamEvent, error) {
	if b == nil || len(b.queries) == 0 && len(b.images) == 0 {
		return nil, nil
	}
	if state == nil {
		return nil, fmt.Errorf("Chat-to-Responses stream state is required")
	}
	events := state.FlushOutputItems()
	for _, image := range b.images {
		imageID := fmt.Sprintf("img_%s", kitutil.GetUUID())
		started, err := state.StartHostedTool(oaichat.HostedToolStreamStart{
			Type: dto.ResponsesOutputTypeImageGenerationCall,
			ID:   imageID,
		})
		if err != nil {
			return nil, err
		}
		events = append(events, started...)
		partial, err := state.AddImagePartial(imageID, 0, image.data)
		if err != nil {
			return nil, err
		}
		events = append(events, partial...)
		completed, err := state.CompleteHostedTool(oaichat.HostedToolStreamResult{
			Type:   dto.ResponsesOutputTypeImageGenerationCall,
			ID:     imageID,
			Result: []byte(image.data),
		})
		if err != nil {
			return nil, err
		}
		events = append(events, completed...)
	}
	if len(b.queries) > 0 {
		action, err := kitutil.Marshal(map[string]any{
			"type":    "search",
			"queries": append([]string(nil), b.queries...),
		})
		if err != nil {
			return nil, fmt.Errorf("marshal Gemini web-search action: %w", err)
		}
		callID := fmt.Sprintf("ws_%s", kitutil.GetUUID())
		started, err := state.StartHostedTool(oaichat.HostedToolStreamStart{
			Type:   "web_search_call",
			ID:     callID,
			Action: action,
		})
		if err != nil {
			return nil, err
		}
		events = append(events, started...)
		completed, err := state.CompleteHostedTool(oaichat.HostedToolStreamResult{
			Type: "web_search_call",
			ID:   callID,
		})
		if err != nil {
			return nil, err
		}
		events = append(events, completed...)
	}
	return events, nil
}
