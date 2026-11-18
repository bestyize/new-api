package controller

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// liteLLMEntry holds the LiteLLM price fields the billing engine can express.
// Every cost is USD per token, character, second, image, pixel or query.
type liteLLMEntry struct {
	Mode                    string   `json:"mode"`
	Provider                string   `json:"litellm_provider"`
	InputCostPerToken       *float64 `json:"input_cost_per_token"`
	OutputCostPerToken      *float64 `json:"output_cost_per_token"`
	CacheReadInputTokenCost *float64 `json:"cache_read_input_token_cost"`
	InputCostPerAudioToken  *float64 `json:"input_cost_per_audio_token"`
	OutputCostPerAudioToken *float64 `json:"output_cost_per_audio_token"`
	InputCostPerImageToken  *float64 `json:"input_cost_per_image_token"`
	OutputCostPerImageToken *float64 `json:"output_cost_per_image_token"`
	InputCostPerCharacter   *float64 `json:"input_cost_per_character"`
	InputCostPerSecond      *float64 `json:"input_cost_per_second"`
	OutputCostPerSecond     *float64 `json:"output_cost_per_second"`
	OutputCostPerImage      *float64 `json:"output_cost_per_image"`
	InputCostPerImage       *float64 `json:"input_cost_per_image"`
	InputCostPerPixel       *float64 `json:"input_cost_per_pixel"`
	InputCostPerQuery       *float64 `json:"input_cost_per_query"`
}

// liteLLMFirstParty lists providers that sell their own models, so their
// entries win over resellers that list the same model at a markup.
var liteLLMFirstParty = map[string]bool{
	"openai": true, "azure": true, "vertex_ai": true, "gemini": true, "anthropic": true,
	"deepseek": true, "xai": true, "mistral": true, "cohere": true, "voyage": true,
	"jina_ai": true, "dashscope": true, "volcengine": true, "moonshot": true, "zai": true,
	"minimax": true, "elevenlabs": true, "deepgram": true, "perplexity": true,
	"black_forest_labs": true, "recraft": true,
}

var liteLLMImageSize = regexp.MustCompile(`^\d+-x-\d+$`)

// The relay counts one minute of audio as 1000 tokens (TTS output and
// transcription input), so a per-second price becomes a per-token one.
const liteLLMAudioTokensPerSecond = 1000.0 / 60

// isLiteLLMPriceEndpoint recognizes LiteLLM's price table by its file name, so
// a mirror of the GitHub raw file works the same.
func isLiteLLMPriceEndpoint(rawURL string) bool {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return path.Base(parsedURL.Path) == liteLLMPriceFile
}

func positiveCost(v *float64) (float64, bool) {
	if v == nil || !isValidNonNegativeCost(*v) || *v == 0 {
		return 0, false
	}
	return *v, true
}

func knownCost(v *float64) (float64, bool) {
	if v == nil || !isValidNonNegativeCost(*v) {
		return 0, false
	}
	return *v, true
}

func ratioFromTokenCost(usd float64) float64 {
	return usd * 1000 * ratio_setting.USD
}

// liteLLMTokenPricing prices a model billed by tokens. Zero-priced entries are
// skipped: LiteLLM lists many placeholders at zero, and importing them would
// make paid models free.
func liteLLMTokenPricing(entry liteLLMEntry) map[string]float64 {
	input, ok := positiveCost(entry.InputCostPerToken)
	if !ok {
		return nil
	}
	pricing := map[string]float64{"model_ratio": ratioFromTokenCost(input)}
	if output, ok := knownCost(entry.OutputCostPerToken); ok {
		pricing["completion_ratio"] = output / input
	}
	if cacheRead, ok := knownCost(entry.CacheReadInputTokenCost); ok {
		pricing["cache_ratio"] = cacheRead / input
	}
	if audioInput, ok := positiveCost(entry.InputCostPerAudioToken); ok {
		pricing["audio_ratio"] = audioInput / input
		if audioOutput, ok := knownCost(entry.OutputCostPerAudioToken); ok {
			pricing["audio_completion_ratio"] = audioOutput / audioInput
		}
	}
	return pricing
}

// liteLLMPricing converts one entry into the local pricing fields, following
// how the relay meters each mode. It returns nil when the entry cannot be
// expressed; video is left out because task plugins price it themselves.
func liteLLMPricing(name string, entry liteLLMEntry) map[string]float64 {
	switch entry.Mode {
	case "chat", "completion", "responses", "embedding", "realtime":
		return liteLLMTokenPricing(entry)
	case "audio_speech":
		// Non-GPT speech models count input characters, so a per-character price
		// maps straight to the ratio and the generated audio is not charged.
		if perChar, ok := positiveCost(entry.InputCostPerCharacter); ok && !strings.Contains(name, "gpt") {
			return map[string]float64{"model_ratio": ratioFromTokenCost(perChar), "audio_completion_ratio": 0}
		}
		input, ok := positiveCost(entry.InputCostPerToken)
		perSecond, hasSeconds := positiveCost(entry.OutputCostPerSecond)
		if !ok || !hasSeconds {
			return nil
		}
		pricing := map[string]float64{
			"model_ratio":            ratioFromTokenCost(input),
			"audio_ratio":            perSecond / liteLLMAudioTokensPerSecond / input,
			"audio_completion_ratio": 1,
		}
		// Streamed speech reports real output tokens instead of a duration.
		if output, ok := knownCost(entry.OutputCostPerAudioToken); ok {
			pricing["completion_ratio"] = output / input
		} else if output, ok := knownCost(entry.OutputCostPerToken); ok {
			pricing["completion_ratio"] = output / input
		}
		return pricing
	case "audio_transcription":
		// Token-priced models return their usage; the others are billed by the
		// uploaded audio's duration.
		if pricing := liteLLMTokenPricing(entry); pricing != nil {
			return pricing
		}
		if perSecond, ok := positiveCost(entry.InputCostPerSecond); ok {
			return map[string]float64{"model_ratio": ratioFromTokenCost(perSecond / liteLLMAudioTokensPerSecond)}
		}
		return nil
	case "image_generation":
		if input, ok := positiveCost(entry.InputCostPerToken); ok {
			output, hasOutput := knownCost(entry.OutputCostPerImageToken)
			if !hasOutput {
				output, hasOutput = knownCost(entry.OutputCostPerToken)
			}
			if hasOutput {
				pricing := map[string]float64{"model_ratio": ratioFromTokenCost(input), "completion_ratio": output / input}
				if imageInput, ok := positiveCost(entry.InputCostPerImageToken); ok {
					pricing["image_ratio"] = imageInput / input
				}
				return pricing
			}
		}
		for _, price := range []*float64{entry.OutputCostPerImage, entry.InputCostPerImage} {
			if perImage, ok := positiveCost(price); ok {
				return map[string]float64{"model_price": perImage}
			}
		}
		// Pixel prices come with the 1024x1024 entries that name the base size.
		if perPixel, ok := positiveCost(entry.InputCostPerPixel); ok {
			return map[string]float64{"model_price": perPixel * 1024 * 1024}
		}
		return nil
	case "rerank":
		if perQuery, ok := positiveCost(entry.InputCostPerQuery); ok {
			return map[string]float64{"model_price": perQuery}
		}
		return liteLLMTokenPricing(entry)
	default:
		return nil
	}
}

// liteLLMModelName maps a LiteLLM key to the model name clients send, and
// ranks it: lower wins when several keys name the same model. Unprefixed keys
// come first, then first-party providers, then resellers; a 1024x1024 image
// entry ranks after the plain model entry, and other sizes are skipped.
func liteLLMModelName(key, provider string) (string, int, bool) {
	parts := strings.Split(key, "/")
	rank := 0
	if len(parts) > 1 && provider != "" && (parts[0] == provider || strings.HasPrefix(provider, parts[0]+"-")) {
		rank = 4
		if liteLLMFirstParty[parts[0]] {
			rank = 2
		}
		parts = parts[1:]
	}
	if len(parts) == 3 && parts[0] == "standard" && parts[1] == "1024-x-1024" {
		return parts[2], rank + 1, true
	}
	if len(parts) == 2 && parts[0] == "1024-x-1024" {
		return parts[1], rank + 1, true
	}
	for _, part := range parts {
		if liteLLMImageSize.MatchString(part) {
			return "", 0, false
		}
	}
	if len(parts) > 2 {
		return "", 0, false
	}
	return strings.Join(parts, "/"), rank, true
}

// convertLiteLLMToRatioData parses LiteLLM's model_prices_and_context_window.json
// into the local ratio format. Unlike models.dev it also carries per-character,
// per-second, per-image and per-query prices, which cover speech, transcription,
// image generation and rerank models:
//
//	model_ratio = input USD per token × 500,000 (per character for speech, per
//	  1000/60 s of audio for duration-billed transcription)
//	audio_ratio = USD per 1000/60 s of speech output ÷ input USD per token
//	model_price = USD per image or per rerank query
func convertLiteLLMToRatioData(reader io.Reader) (map[string]any, error) {
	var raw map[string]json.RawMessage
	if err := common.DecodeJson(reader, &raw); err != nil {
		return nil, fmt.Errorf("failed to decode LiteLLM price table: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty LiteLLM price table")
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	type candidate struct {
		rank    int
		pricing map[string]float64
	}
	selected := make(map[string]candidate)
	for _, key := range keys {
		var entry liteLLMEntry
		// One malformed entry must not drop the whole table.
		if err := common.Unmarshal(raw[key], &entry); err != nil {
			continue
		}
		name, rank, ok := liteLLMModelName(key, entry.Provider)
		if !ok {
			continue
		}
		pricing := liteLLMPricing(name, entry)
		if pricing == nil {
			continue
		}
		if current, exists := selected[name]; exists && current.rank <= rank {
			continue
		}
		selected[name] = candidate{rank: rank, pricing: pricing}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no valid LiteLLM pricing entries found")
	}

	converted := make(map[string]any)
	for name, chosen := range selected {
		for field, value := range chosen.pricing {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
				continue
			}
			entries, _ := converted[field].(map[string]any)
			if entries == nil {
				entries = make(map[string]any)
				converted[field] = entries
			}
			entries[name] = roundRatioValue(value)
		}
	}
	return converted, nil
}
