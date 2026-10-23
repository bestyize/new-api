package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
)

func TestPricingSyncExpressionPriority(t *testing.T) {
	expression := `tier("base", p * 2 + c * 8 + cr * 0)`
	cases := []struct {
		name       string
		local      map[string]any
		source     map[string]any
		wantFields []string
	}{
		{"equal expressions suppress stale ratios", map[string]any{"billing_mode": map[string]string{"m": "tiered_expr"}, "billing_expr": map[string]string{"m": expression}}, map[string]any{"billing_mode": map[string]string{"m": "tiered_expr"}, "billing_expr": map[string]string{"m": expression}, "model_ratio": map[string]float64{"m": 3}, "model_price": map[string]float64{"m": 2}}, nil},
		{"local expression excludes legacy-only source", map[string]any{"billing_mode": map[string]string{"m": "tiered_expr"}, "billing_expr": map[string]string{"m": expression}}, map[string]any{"model_ratio": map[string]float64{"m": 3}, "completion_ratio": map[string]float64{"m": 2}}, nil},
		{"expression imports without legacy conflicts", map[string]any{"model_ratio": map[string]float64{"m": 1}}, map[string]any{"billing_mode": map[string]string{"m": "tiered_expr"}, "billing_expr": map[string]string{"m": expression}, "model_ratio": map[string]float64{"m": 3}, "model_price": map[string]float64{"m": 2}}, []string{"billing_mode", "billing_expr"}},
		{"inactive expression follows explicit ratio mode", map[string]any{"model_ratio": map[string]float64{"m": 1}}, map[string]any{"billing_mode": map[string]string{"m": "ratio"}, "billing_expr": map[string]string{"m": expression}, "model_ratio": map[string]float64{"m": 3}}, []string{"model_ratio"}},
		{"empty active expression never imports a false free price", map[string]any{}, map[string]any{"billing_mode": map[string]string{"m": "tiered_expr"}, "billing_expr": map[string]string{"m": " "}, "model_ratio": map[string]float64{"m": 0}}, nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			diff := buildDifferences(tt.local, []struct {
				name string
				data map[string]any
			}{{"source", tt.source}})
			fields := make([]string, 0, len(diff["m"]))
			for field := range diff["m"] {
				fields = append(fields, field)
			}
			assert.ElementsMatch(t, tt.wantFields, fields)
		})
	}
}

func TestRatioConfigExportsEffectiveExpressions(t *testing.T) {
	before := config.GlobalConfig.ExportAllConfigs()
	expose := ratio_setting.IsExposeRatioEnabled()
	t.Cleanup(func() {
		config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{"billing_mode": before["billing_setting.billing_mode"], "billing_expr": before["billing_setting.billing_expr"]})
		ratio_setting.SetExposeRatioEnabled(expose)
	})
	config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{"billing_mode": `{"sync-export":"tiered_expr"}`, "billing_expr": `{"sync-export":"tier(\"base\", p * 2)"}`})
	ratio_setting.SetExposeRatioEnabled(true)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/ratio_config", nil)
	GetRatioConfig(c)
	var response struct {
		Success bool
		Data    map[string]any
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, billing_setting.BillingModeTieredExpr, valueMap(response.Data["billing_mode"])["sync-export"])
	assert.Equal(t, `tier("base", p * 2)`, valueMap(response.Data["billing_expr"])["sync-export"])
}

func TestPricingSyncCompleteSourcesAndArrayFormats(t *testing.T) {
	before := config.GlobalConfig.ExportAllConfigs()
	oldRatios, oldCompletion := ratio_setting.ModelRatio2JSONString(), ratio_setting.CompletionRatio2JSONString()
	t.Cleanup(func() {
		config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{"billing_mode": before["billing_setting.billing_mode"], "billing_expr": before["billing_setting.billing_expr"]})
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(oldCompletion))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"sync-token":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"sync-token":2}`))
	expression := `len <= 200000 ? tier("short", p * 2 + c * 8 + cr * 0) : tier("long", p * 4 + c * 12)`
	expressions, err := common.Marshal(map[string]string{"sync-already": expression})
	require.NoError(t, err)
	config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{"billing_mode": `{"sync-already":"tiered_expr"}`, "billing_expr": string(expressions)})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		if r.URL.Path == "/ratio_config" {
			data = map[string]any{
				"billing_mode":     map[string]string{"sync-already": "tiered_expr", "sync-expression": "tiered_expr"},
				"billing_expr":     map[string]string{"sync-already": expression, "sync-expression": expression},
				"model_ratio":      map[string]float64{"sync-already": 9, "sync-expression": 9, "sync-token": 1},
				"model_price":      map[string]float64{"sync-expression": 4},
				"completion_ratio": map[string]float64{"sync-token": 4},
				"cache_ratio":      map[string]float64{"sync-token": 0},
			}
		} else {
			data = []map[string]any{
				{"model_name": "sync-already", "model_ratio": 5, "completion_ratio": 3},
				{"model_name": "sync-expression", "model_ratio": 2, "model_price": 1},
				{"model_name": "sync-array-expression", "billing_mode": "tiered_expr", "billing_expr": expression, "quota_type": 1, "model_price": 0},
				{"model_name": "sync-unpriced"},
				{"model_name": "sync-invalid-expression", "billing_mode": "tiered_expr", "billing_expr": "", "model_ratio": 0},
				{"model_name": "sync-free", "model_ratio": 0, "completion_ratio": 0},
			}
		}
		encoded, err := common.Marshal(map[string]any{"success": true, "data": data})
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(encoded)
	}))
	defer server.Close()
	var response struct {
		Success bool
		Data    struct {
			Differences map[string]map[string]dto.DifferenceItem
			Prices      map[string]struct {
				Current   map[string]any
				Upstreams map[string]map[string]any
			}
			TestResults []dto.TestResult `json:"test_results"`
		}
	}
	body := map[string]any{"upstreams": []map[string]any{
		{"id": 1, "name": "Expressions", "base_url": server.URL, "endpoint": "/ratio_config"},
		{"id": 2, "name": "Legacy", "base_url": server.URL, "endpoint": "/pricing"},
	}}
	recorder := modelManagementRequest(t, FetchUpstreamRatios, http.MethodPost, "/api/channel/fetch_upstream_ratios", body, &response)
	require.True(t, response.Success, recorder.Body.String())
	require.Len(t, response.Data.TestResults, 2)
	for _, result := range response.Data.TestResults {
		require.Equal(t, "success", result.Status, result.Error)
	}
	assert.NotContains(t, response.Data.Differences, "sync-already")
	assert.NotContains(t, response.Data.Differences, "sync-unpriced")
	assert.NotContains(t, response.Data.Differences, "sync-invalid-expression")
	assert.Equal(t, map[string]any{"billing_mode": "tiered_expr", "billing_expr": expression}, response.Data.Prices["sync-expression"].Upstreams["Expressions(1)"])
	assert.NotContains(t, response.Data.Prices["sync-expression"].Upstreams, "Legacy(2)")
	assert.Equal(t, map[string]any{"billing_mode": "tiered_expr", "billing_expr": expression}, response.Data.Prices["sync-array-expression"].Upstreams["Legacy(2)"])
	assert.Equal(t, float64(1), response.Data.Prices["sync-token"].Upstreams["Expressions(1)"]["model_ratio"], "unchanged base prices are included for a complete price preview")
	assert.Equal(t, float64(4), response.Data.Prices["sync-token"].Upstreams["Expressions(1)"]["completion_ratio"])
	assert.Equal(t, float64(0), response.Data.Prices["sync-token"].Upstreams["Expressions(1)"]["cache_ratio"])
	assert.Equal(t, float64(0), response.Data.Prices["sync-free"].Upstreams["Legacy(2)"]["model_ratio"])
	assert.NotContains(t, response.Data.Prices, "sync-already")
	body["include_unchanged"] = true
	recorder = modelManagementRequest(t, FetchUpstreamRatios, http.MethodPost, "/api/ratio_sync/fetch", body, &response)
	require.True(t, response.Success, recorder.Body.String())
	assert.Equal(t, map[string]any{"billing_mode": "tiered_expr", "billing_expr": expression}, response.Data.Prices["sync-already"].Upstreams["Expressions(1)"])
	assert.NotContains(t, response.Data.Prices["sync-already"].Upstreams, "Legacy(2)")
	assert.NotContains(t, response.Data.Prices, "sync-unpriced")
	assert.NotContains(t, response.Data.Prices, "sync-invalid-expression")
	assert.NotContains(t, response.Data.Differences, "sync-already")
}

func TestPricingSyncLiteLLMPresetConvertsMediaPrices(t *testing.T) {
	table := `{
		"sample_spec": {"mode": "chat", "input_cost_per_token": 0.0, "search_context_cost_per_query": {"search_context_size_low": 0.0}},
		"whisper-1": {"mode": "audio_transcription", "litellm_provider": "openai", "input_cost_per_second": 0.0001, "output_cost_per_second": 0.0001},
		"gpt-4o-transcribe": {"mode": "audio_transcription", "litellm_provider": "openai", "input_cost_per_token": 2.5e-06, "input_cost_per_audio_token": 2.5e-06, "output_cost_per_token": 1e-05, "input_cost_per_second": 0.0001},
		"tts-1": {"mode": "audio_speech", "litellm_provider": "openai", "input_cost_per_character": 1.5e-05},
		"gpt-4o-mini-tts": {"mode": "audio_speech", "litellm_provider": "openai", "input_cost_per_token": 6e-07, "output_cost_per_token": 1e-05, "output_cost_per_audio_token": 1.2e-05, "output_cost_per_second": 0.00025},
		"gpt-realtime": {"mode": "realtime", "litellm_provider": "openai", "input_cost_per_token": 4e-06, "output_cost_per_token": 1.6e-05, "input_cost_per_audio_token": 3.2e-05, "output_cost_per_audio_token": 6.4e-05},
		"gpt-image-1": {"mode": "image_generation", "litellm_provider": "openai", "input_cost_per_token": 5e-06, "input_cost_per_image_token": 1e-05, "output_cost_per_image_token": 4e-05},
		"low/1024-x-1024/gpt-image-1": {"mode": "image_generation", "litellm_provider": "openai", "input_cost_per_image": 0.011},
		"dall-e-3": {},
		"aiml/dall-e-3": {"mode": "image_generation", "litellm_provider": "aiml", "output_cost_per_image": 0.052},
		"azure/hd/1024-x-1024/dall-e-3": {"mode": "image_generation", "litellm_provider": "azure", "input_cost_per_pixel": 8.39233e-08},
		"azure/standard/1024-x-1024/dall-e-3": {"mode": "image_generation", "litellm_provider": "azure", "input_cost_per_pixel": 4.19617e-08},
		"rerank-v3.5": {"mode": "rerank", "litellm_provider": "cohere", "input_cost_per_query": 0.002, "input_cost_per_token": 0.0},
		"sora-2": {"mode": "video_generation", "litellm_provider": "openai", "output_cost_per_video_per_second": 0.1},
		"broken-price": {"mode": "chat", "input_cost_per_token": "unknown"}
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/BerriAI/litellm/main/model_prices_and_context_window.json", r.URL.Path)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(table))
	}))
	defer server.Close()
	var response struct {
		Success bool
		Data    struct {
			Prices map[string]struct {
				Upstreams map[string]map[string]any
			}
			TestResults []dto.TestResult `json:"test_results"`
		}
	}
	body := map[string]any{"include_unchanged": true, "upstreams": []map[string]any{
		{"id": liteLLMPresetID, "name": liteLLMPresetName, "base_url": server.URL, "endpoint": server.URL + "/BerriAI/litellm/main/model_prices_and_context_window.json"},
	}}
	recorder := modelManagementRequest(t, FetchUpstreamRatios, http.MethodPost, "/api/ratio_sync/fetch", body, &response)
	require.True(t, response.Success, recorder.Body.String())
	require.Len(t, response.Data.TestResults, 1)
	require.Equal(t, "success", response.Data.TestResults[0].Status, response.Data.TestResults[0].Error)

	source := liteLLMPresetName + "(-102)"
	cases := []struct {
		model string
		want  map[string]any
	}{
		// One minute of uploaded audio is billed as 1000 tokens: $0.006/min.
		{"whisper-1", map[string]any{"billing_mode": "ratio", "model_ratio": 3.0}},
		{"gpt-4o-transcribe", map[string]any{"billing_mode": "ratio", "model_ratio": 1.25, "completion_ratio": 4.0, "audio_ratio": 1.0}},
		// Characters are counted as input tokens; the generated audio is free.
		{"tts-1", map[string]any{"billing_mode": "ratio", "model_ratio": 7.5, "audio_completion_ratio": 0.0}},
		// $0.015 per minute of speech is 1000 audio tokens at 0.3 × 25.
		{"gpt-4o-mini-tts", map[string]any{"billing_mode": "ratio", "model_ratio": 0.3, "completion_ratio": 20.0, "audio_ratio": 25.0, "audio_completion_ratio": 1.0}},
		{"gpt-realtime", map[string]any{"billing_mode": "ratio", "model_ratio": 2.0, "completion_ratio": 4.0, "audio_ratio": 8.0, "audio_completion_ratio": 2.0}},
		{"gpt-image-1", map[string]any{"billing_mode": "ratio", "model_ratio": 2.5, "completion_ratio": 8.0, "image_ratio": 2.0}},
		// The first-party 1024x1024 standard price wins over a reseller's.
		{"dall-e-3", map[string]any{"billing_mode": "ratio", "model_price": 0.044}},
		{"rerank-v3.5", map[string]any{"billing_mode": "ratio", "model_price": 0.002}},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			assert.Equal(t, tc.want, response.Data.Prices[tc.model].Upstreams[source])
		})
	}
	for _, skipped := range []string{"sample_spec", "sora-2", "broken-price", "low/1024-x-1024/gpt-image-1", "aiml/dall-e-3"} {
		assert.NotContains(t, response.Data.Prices, skipped)
	}
	assert.True(t, isLiteLLMPriceEndpoint("https://cdn.jsdelivr.net/gh/BerriAI/litellm@main/model_prices_and_context_window.json"), "a mirror of the table is recognized")
	assert.False(t, isLiteLLMPriceEndpoint("https://models.dev/api.json"))
}
