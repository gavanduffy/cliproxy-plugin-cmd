package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// fallbackHTTPClient is a fallback HostHTTPClient implementation that uses the
// standard net/http client. It is used when the CLIProxyAPI host does not
// inject an HTTP client (e.g., during standalone tests).
type fallbackHTTPClient struct {
	client *http.Client
}

var (
	fallbackHTTPClientInstance *fallbackHTTPClient
	fallbackHTTPClientOnce     sync.Once
)

func newFallbackHTTPClient() *fallbackHTTPClient {
	fallbackHTTPClientOnce.Do(func() {
		fallbackHTTPClientInstance = &fallbackHTTPClient{
			client: newHTTPClient(),
		}
	})
	return fallbackHTTPClientInstance
}

func (c *fallbackHTTPClient) Do(ctx context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return pluginapi.HTTPResponse{}, err
	}
	httpReq.Header = req.Headers
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return pluginapi.HTTPResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return pluginapi.HTTPResponse{}, err
	}
	return pluginapi.HTTPResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header.Clone(),
		Body:       body,
	}, nil
}

func (c *fallbackHTTPClient) DoStream(ctx context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return pluginapi.HTTPStreamResponse{}, err
	}
	httpReq.Header = req.Headers
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return pluginapi.HTTPStreamResponse{}, err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return pluginapi.HTTPStreamResponse{}, fmt.Errorf("Command Code API error %d: %s", resp.StatusCode, string(body))
	}

	chunks := make(chan pluginapi.HTTPStreamChunk, 16)
	go func() {
		defer close(chunks)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			select {
			case chunks <- pluginapi.HTTPStreamChunk{Payload: []byte(scanner.Text() + "\n")}:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case chunks <- pluginapi.HTTPStreamChunk{Err: err}:
			case <-ctx.Done():
			}
		}
	}()

	return pluginapi.HTTPStreamResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header.Clone(),
		Chunks:     chunks,
	}, nil
}

const (
	commandCodeAPIBase = "https://api.commandcode.ai"
	maxOutputTokens    = 65536
)

func handleExecute(request []byte, stream bool) ([]byte, error) {
	var req pluginapi.ExecutorRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, err
	}

	apiKey := getAPIKey(req)
	if apiKey == "" {
		return nil, fmt.Errorf("no Command Code API key available")
	}

	apiBase := getPluginConfig().APIBase
	if v := os.Getenv("COMMANDCODE_API_BASE"); v != "" {
		apiBase = v
	}

	ccReq, err := buildCommandCodeRequest(req, stream)
	if err != nil {
		return nil, err
	}

	if stream {
		return executeStream(apiBase, apiKey, ccReq, req)
	}
	return executeNonStream(apiBase, apiKey, ccReq, req)
}

func getAPIKey(req pluginapi.ExecutorRequest) string {
	if v := req.AuthAttributes["api_key"]; v != "" {
		return v
	}
	if v := req.AuthMetadata["api_key"]; v != "" {
		if s, ok := v.(string); ok {
			return s
		}
	}
	if v := os.Getenv("COMMANDCODE_API_KEY"); v != "" {
		return v
	}
	return ""
}

func buildCommandCodeRequest(req pluginapi.ExecutorRequest, stream bool) (ccGenerateRequest, error) {
	var openAIReq openAIChatCompletionRequest
	if err := json.Unmarshal(req.Payload, &openAIReq); err != nil {
		return ccGenerateRequest{}, err
	}

	var systemPrompt string
	messages := make([]ccMessage, 0, len(openAIReq.Messages))
	for _, m := range openAIReq.Messages {
		if m.Role == "system" {
			if s, ok := m.Content.(string); ok {
				systemPrompt += s + "\n"
			}
			continue
		}
		messages = append(messages, ccMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	maxTokens := openAIReq.MaxTokens
	if maxTokens <= 0 {
		maxTokens = maxOutputTokens
	}
	if maxTokens > maxOutputTokens {
		maxTokens = maxOutputTokens
	}

	temperature := openAIReq.Temperature
	if temperature == 0 {
		temperature = 0.3
	}

	return ccGenerateRequest{
		Config: ccConfig{
			WorkingDir:  "/tmp",
			Date:        time.Now().UTC().Format("2006-01-02"),
			Environment: "linux-amd64, CLIProxyAPI",
		},
		Params: ccGenerateParams{
			Model:       openAIReq.Model,
			Messages:    messages,
			Tools:       openAIReq.Tools,
			System:      strings.TrimSpace(systemPrompt),
			MaxTokens:   maxTokens,
			Temperature: temperature,
			Stream:      stream,
		},
		ThreadID: uuid.New().String(),
	}, nil
}

func executeNonStream(apiBase, apiKey string, ccReq ccGenerateRequest, req pluginapi.ExecutorRequest) ([]byte, error) {
	body, err := json.Marshal(ccReq)
	if err != nil {
		return nil, err
	}

	httpReq := pluginapi.HTTPRequest{
		Method: http.MethodPost,
		URL:    apiBase + "/alpha/generate",
		Headers: http.Header{
			"Content-Type":           []string{"application/json"},
			"Authorization":          []string{"Bearer " + apiKey},
			"x-command-code-version": []string{commandCodeVersion},
			"x-cli-environment":      []string{"production"},
		},
		Body: body,
	}

	ctx, cancel := context.WithCancel(lifecycleCtx)
	defer cancel()

	var resp pluginapi.HTTPResponse
	if req.HTTPClient != nil {
		resp, err = req.HTTPClient.Do(ctx, httpReq)
	} else {
		resp, err = newFallbackHTTPClient().Do(ctx, httpReq)
	}
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Command Code API error %d: %s", resp.StatusCode, string(resp.Body))
	}

	// For non-streaming, collect all text deltas and return a single completion.
	var fullText, fullReasoning strings.Builder
	for _, line := range strings.Split(string(resp.Body), "\n") {
		event, ok := parseStreamEvent(line)
		if !ok {
			continue
		}
		switch event.Type {
		case "text-delta":
			fullText.WriteString(event.Text)
		case "reasoning-delta":
			fullReasoning.WriteString(event.Text)
			fullText.WriteString(event.Text) // keep in content for compatibility
		case "error":
			return nil, fmt.Errorf("Command Code stream error: %v", event.Error)
		}
	}

	completion := openAIChatCompletion{
		ID:      "cc-" + uuid.New().String(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   ccReq.Params.Model,
		Choices: []openAIChoice{
			{
				Index: 0,
				Delta: openAIDelta{
					Role:      "assistant",
					Content:   fullText.String(),
					Reasoning: fullReasoning.String(),
				},
				FinishReason: strPtr("stop"),
			},
		},
	}

	payload, err := json.Marshal(completion)
	if err != nil {
		return nil, err
	}

	return okEnvelope(pluginapi.ExecutorResponse{Payload: payload})
}

func executeStream(apiBase, apiKey string, ccReq ccGenerateRequest, req pluginapi.ExecutorRequest) ([]byte, error) {
	body, err := json.Marshal(ccReq)
	if err != nil {
		return nil, err
	}

	httpReq := pluginapi.HTTPRequest{
		Method: http.MethodPost,
		URL:    apiBase + "/alpha/generate",
		Headers: http.Header{
			"Content-Type":           []string{"application/json"},
			"Authorization":          []string{"Bearer " + apiKey},
			"x-command-code-version": []string{commandCodeVersion},
			"x-cli-environment":      []string{"production"},
			"Accept":                 []string{"text/event-stream"},
		},
		Body: body,
	}

	ctx, cancel := context.WithCancel(lifecycleCtx)
	defer cancel()

	var resp pluginapi.HTTPStreamResponse
	if req.HTTPClient != nil {
		resp, err = req.HTTPClient.DoStream(ctx, httpReq)
	} else {
		resp, err = newFallbackHTTPClient().DoStream(ctx, httpReq)
	}
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Command Code API error %d", resp.StatusCode)
	}

	var chunks []pluginapi.ExecutorStreamChunk
	id := "cc-" + uuid.New().String()
	var buffer strings.Builder

	for chunk := range resp.Chunks {
		if chunk.Err != nil {
			return nil, fmt.Errorf("stream read error: %w", chunk.Err)
		}

		buffer.Write(chunk.Payload)
		if !strings.HasSuffix(buffer.String(), "\n") {
			continue
		}

		event, ok := parseStreamEvent(buffer.String())
		buffer.Reset()
		if !ok {
			continue
		}

		if event.Type == "finish" {
			final := openAIChatCompletionChunk{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: time.Now().Unix(),
				Model:   ccReq.Params.Model,
				Choices: []openAIChoice{{
					Index:        0,
					Delta:        openAIDelta{},
					FinishReason: strPtr("stop"),
				}},
			}
			data, _ := json.Marshal(final)
			chunks = append(chunks, pluginapi.ExecutorStreamChunk{Payload: []byte("data: " + string(data) + "\n\n")})
			break
		}

		if event.Type == "error" {
			errMsg := "Command Code stream error"
			if s, ok := event.Error.(string); ok && s != "" {
				errMsg = s
			}
			return nil, fmt.Errorf("%s", errMsg)
		}

		chunkPayload := commandCodeEventToOpenAI(id, ccReq.Params.Model, event)
		if chunkPayload != nil {
			chunks = append(chunks, pluginapi.ExecutorStreamChunk{Payload: chunkPayload})
		}
	}

	chunks = append(chunks, pluginapi.ExecutorStreamChunk{Payload: []byte("data: [DONE]\n\n")})

	return okEnvelope(streamResponse{
		Headers: http.Header{"Content-Type": []string{"text/event-stream"}},
		Chunks:  chunks,
	})
}

func handleHTTPRequest(request []byte) ([]byte, error) {
	var req pluginapi.ExecutorHTTPRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, err
	}

	apiKey := ""
	if req.Attributes != nil {
		apiKey = req.Attributes["api_key"]
	}
	if apiKey == "" && req.Metadata != nil {
		if v, ok := req.Metadata["api_key"].(string); ok {
			apiKey = v
		}
	}
	if apiKey == "" {
		apiKey = os.Getenv("COMMANDCODE_API_KEY")
	}

	httpReq := pluginapi.HTTPRequest{
		Method:  req.Method,
		URL:     req.URL,
		Headers: req.Headers.Clone(),
		Body:    req.Body,
	}
	if apiKey != "" {
		httpReq.Headers.Set("Authorization", "Bearer "+apiKey)
	}
	httpReq.Headers.Set("x-command-code-version", commandCodeVersion)
	httpReq.Headers.Set("x-cli-environment", "production")

	ctx, cancel := context.WithCancel(lifecycleCtx)
	defer cancel()

	var resp pluginapi.HTTPResponse
	var err error
	if req.HTTPClient != nil {
		resp, err = req.HTTPClient.Do(ctx, httpReq)
	} else {
		resp, err = newFallbackHTTPClient().Do(ctx, httpReq)
	}
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Command Code HTTP error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return okEnvelope(pluginapi.ExecutorHTTPResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Headers,
		Body:       resp.Body,
	})
}

func parseStreamEvent(line string) (ccStreamEvent, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, ":") {
		return ccStreamEvent{}, false
	}
	if strings.HasPrefix(trimmed, "data:") {
		trimmed = strings.TrimSpace(trimmed[5:])
	}
	if trimmed == "" || trimmed == "[DONE]" {
		return ccStreamEvent{}, false
	}

	var event ccStreamEvent
	if err := json.Unmarshal([]byte(trimmed), &event); err != nil {
		return ccStreamEvent{}, false
	}
	return event, true
}

func commandCodeEventToOpenAI(id, model string, event ccStreamEvent) []byte {
	var delta openAIDelta
	switch event.Type {
	case "text-delta":
		delta = openAIDelta{Content: event.Text}
	case "reasoning-delta":
		// Expose reasoning in its own field while also keeping it in content
		// for clients that do not understand the reasoning field.
		delta = openAIDelta{
			Content:   event.Text,
			Reasoning: event.Text,
		}
	case "reasoning-start", "reasoning-end":
		// These are lifecycle events with no payload; do not emit a chunk.
		return nil
	case "tool-call":
		delta = openAIDelta{
			ToolCalls: []any{
				map[string]any{
					"id":   event.ToolCallID,
					"type": "function",
					"function": map[string]any{
						"name":      event.ToolName,
						"arguments": jsonString(event.Input),
					},
				},
			},
		}
	case "finish", "error":
		return nil
	default:
		return nil
	}

	chunk := openAIChatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []openAIChoice{{Index: 0, Delta: delta}},
	}

	data, _ := json.Marshal(chunk)
	return []byte("data: " + string(data) + "\n\n")
}

func jsonString(v any) string {
	if v == nil {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func strPtr(s string) *string {
	return &s
}
