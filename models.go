package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const (
	defaultModelsURL = "https://api.commandcode.ai/provider/v1/models"
)

// Static cost table copied from the pi provider.
var modelCosts = map[string]modelCost{
	"claude-opus-4-7":            {input: 5, output: 25, cacheRead: 0.5, cacheWrite: 6.25},
	"claude-opus-4-6":            {input: 5, output: 25, cacheRead: 0.5, cacheWrite: 6.25},
	"claude-sonnet-4-6":          {input: 3, output: 15, cacheRead: 0.3, cacheWrite: 3.75},
	"claude-haiku-4-5-20251001":  {input: 1, output: 5, cacheRead: 0.1, cacheWrite: 1.25},
	"gpt-5.5":                    {input: 5, output: 30, cacheRead: 0.5, cacheWrite: 0},
	"gpt-5.4":                    {input: 2.5, output: 15, cacheRead: 0.25, cacheWrite: 0},
	"gpt-5.3-codex":              {input: 2, output: 8, cacheRead: 0.5, cacheWrite: 0},
	"gpt-5.4-mini":               {input: 0.75, output: 4.5, cacheRead: 0.075, cacheWrite: 0},
	"google/gemini-3.5-flash":    {input: 1.5, output: 9, cacheRead: 0.15, cacheWrite: 0},
	"google/gemini-3.1-flash-lite": {input: 0.25, output: 1.5, cacheRead: 0.03, cacheWrite: 0},
	"deepseek/deepseek-v4-pro":   {input: 0.435, output: 0.87, cacheRead: 0.003625, cacheWrite: 0},
	"deepseek/deepseek-v4-flash": {input: 0.14, output: 0.28, cacheRead: 0.028, cacheWrite: 0},
	"moonshotai/Kimi-K2.6":       {input: 0.95, output: 4, cacheRead: 0.16, cacheWrite: 0},
	"moonshotai/Kimi-K2.5":       {input: 0.6, output: 3, cacheRead: 0.1, cacheWrite: 0},
	"zai-org/GLM-5.1":            {input: 1.4, output: 4.4, cacheRead: 0.26, cacheWrite: 0},
	"zai-org/GLM-5":              {input: 1, output: 3.2, cacheRead: 0.2, cacheWrite: 0},
	"MiniMaxAI/MiniMax-M2.7":     {input: 0.3, output: 1.2, cacheRead: 0.06, cacheWrite: 0},
	"MiniMaxAI/MiniMax-M2.5":     {input: 0.27, output: 0.95, cacheRead: 0.03, cacheWrite: 0},
	"Qwen/Qwen3.6-Max-Preview":   {input: 1.3, output: 7.8, cacheRead: 0.26, cacheWrite: 1.63},
	"Qwen/Qwen3.6-Plus":          {input: 0.5, output: 3, cacheRead: 0.1, cacheWrite: 0},
	"Qwen/Qwen3.7-Max":           {input: 1.25, output: 3.75, cacheRead: 0.25, cacheWrite: 1.56},
	"stepfun/Step-3.5-Flash":     {input: 0.1, output: 0.3, cacheRead: 0.02, cacheWrite: 0},
	"xiaomi/mimo-v2.5-pro":       {input: 0, output: 0, cacheRead: 0, cacheWrite: 0},
	"xiaomi/mimo-v2.5":           {input: 0, output: 0, cacheRead: 0, cacheWrite: 0},
}

type modelCost struct {
	input      float64
	output     float64
	cacheRead  float64
	cacheWrite float64
}

func handleModels(request []byte) ([]byte, error) {
	var req pluginapi.StaticModelRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, err
	}

	modelsURL := getPluginConfig().ModelsURL
	if envURL := os.Getenv("COMMANDCODE_MODELS_URL"); envURL != "" {
		modelsURL = envURL
	}

	models, err := fetchCommandCodeModels(lifecycleCtx, modelsURL)
	if err != nil {
		// Fall back to static model list if fetch fails.
		models = staticModels()
	}

	return okEnvelope(pluginapi.ModelResponse{
		Provider: pluginName,
		Models:   models,
	})
}

func fetchCommandCodeModels(ctx context.Context, url string) ([]pluginapi.ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	client := newHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models endpoint returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var apiResp ccModelsResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, err
	}

	models := make([]pluginapi.ModelInfo, 0, len(apiResp.Data))
	for _, m := range apiResp.Data {
		models = append(models, toPluginModel(m))
	}
	return models, nil
}

func toPluginModel(m ccModel) pluginapi.ModelInfo {
	maxTokens := int64(65536)
	if m.ContextLength > 0 && m.ContextLength < maxTokens {
		maxTokens = m.ContextLength
	}
	return pluginapi.ModelInfo{
		ID:                         m.ID,
		Object:                     "model",
		OwnedBy:                    pluginName,
		Type:                       pluginName,
		DisplayName:                m.Name + " (CC)",
		Name:                       m.ID,
		ContextLength:              m.ContextLength,
		MaxCompletionTokens:        maxTokens,
		SupportedGenerationMethods: []string{"chat"},
		UserDefined:                false,
	}
}

func staticModels() []pluginapi.ModelInfo {
	models := make([]pluginapi.ModelInfo, 0, len(modelCosts))
	for id := range modelCosts {
		models = append(models, pluginapi.ModelInfo{
			ID:                         id,
			Object:                     "model",
			OwnedBy:                    pluginName,
			Type:                       pluginName,
			DisplayName:                id + " (CC)",
			Name:                       id,
			ContextLength:              128000,
			MaxCompletionTokens:        65536,
			SupportedGenerationMethods: []string{"chat"},
		})
	}
	return models
}
