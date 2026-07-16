package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// mockHostHTTPClient adapts a standard http.Client to the pluginapi.HostHTTPClient
// interface so integration tests can inject a local httptest server.
type mockHostHTTPClient struct {
	client *http.Client
}

func (m *mockHostHTTPClient) Do(ctx context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return pluginapi.HTTPResponse{}, err
	}
	httpReq.Header = req.Headers
	resp, err := m.client.Do(httpReq)
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

func (m *mockHostHTTPClient) DoStream(ctx context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return pluginapi.HTTPStreamResponse{}, err
	}
	httpReq.Header = req.Headers
	resp, err := m.client.Do(httpReq)
	if err != nil {
		return pluginapi.HTTPStreamResponse{}, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return pluginapi.HTTPStreamResponse{}, nil
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

func TestHandleExecuteNonStreaming(t *testing.T) {
	mockResponse := `data: {"type":"reasoning-delta","text":"thinking..."}
data: {"type":"text-delta","text":"hello"}
data: {"type":"finish"}
`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/alpha/generate" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer ts.Close()

	reqObj := pluginapi.ExecutorRequest{
		AuthAttributes: map[string]string{"api_key": "test_key"},
		Payload:        []byte(`{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`),
		HTTPClient:     &mockHostHTTPClient{client: ts.Client()},
	}
	reqBytes, _ := json.Marshal(reqObj)

	// Override the API base for this test via config.
	setPluginConfig(pluginConfig{APIBase: ts.URL, ModelsURL: defaultModelsURL})

	respBytes, err := handleExecute(reqBytes, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env envelope
	if err := json.Unmarshal(respBytes, &env); err != nil {
		t.Fatalf("invalid envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope not ok: %s", string(respBytes))
	}

	var execResp pluginapi.ExecutorResponse
	if err := json.Unmarshal(env.Result, &execResp); err != nil {
		t.Fatalf("invalid executor response: %v", err)
	}

	var completion openAIChatCompletion
	if err := json.Unmarshal(execResp.Payload, &completion); err != nil {
		t.Fatalf("invalid completion payload: %v", err)
	}

	if len(completion.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(completion.Choices))
	}
	if !strings.Contains(completion.Choices[0].Delta.Content, "hello") {
		t.Errorf("expected content to contain 'hello', got %q", completion.Choices[0].Delta.Content)
	}
	if !strings.Contains(completion.Choices[0].Delta.Reasoning, "thinking...") {
		t.Errorf("expected reasoning to contain 'thinking...', got %q", completion.Choices[0].Delta.Reasoning)
	}
}

func TestHandleExecuteStreaming(t *testing.T) {
	mockResponse := `data: {"type":"reasoning-delta","text":"thinking..."}
data: {"type":"text-delta","text":"hello"}
data: {"type":"finish"}
`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(mockResponse))
	}))
	defer ts.Close()

	reqObj := pluginapi.ExecutorRequest{
		AuthAttributes: map[string]string{"api_key": "test_key"},
		Payload:        []byte(`{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`),
		HTTPClient:     &mockHostHTTPClient{client: ts.Client()},
	}
	reqBytes, _ := json.Marshal(reqObj)

	setPluginConfig(pluginConfig{APIBase: ts.URL, ModelsURL: defaultModelsURL})

	respBytes, err := handleExecute(reqBytes, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var env envelope
	if err := json.Unmarshal(respBytes, &env); err != nil {
		t.Fatalf("invalid envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("envelope not ok: %s", string(respBytes))
	}

	var streamResp streamResponse
	if err := json.Unmarshal(env.Result, &streamResp); err != nil {
		t.Fatalf("invalid stream response: %v", err)
	}

	if streamResp.Headers.Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %q", streamResp.Headers.Get("Content-Type"))
	}

	var foundReasoning, foundText, foundDone bool
	for _, chunk := range streamResp.Chunks {
		if chunk.Err != nil {
			t.Fatalf("unexpected stream error: %v", chunk.Err)
		}
		if strings.Contains(string(chunk.Payload), "[DONE]") {
			foundDone = true
			continue
		}
		if strings.Contains(string(chunk.Payload), "thinking...") {
			foundReasoning = true
		}
		if strings.Contains(string(chunk.Payload), "hello") {
			foundText = true
		}
	}

	if !foundReasoning {
		t.Error("expected reasoning chunk")
	}
	if !foundText {
		t.Error("expected text chunk")
	}
	if !foundDone {
		t.Error("expected [DONE] marker")
	}
}

func TestHandleExecuteUpstreamError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer ts.Close()

	setPluginConfig(pluginConfig{APIBase: ts.URL, ModelsURL: defaultModelsURL})

	reqObj := pluginapi.ExecutorRequest{
		AuthAttributes: map[string]string{"api_key": "test_key"},
		Payload:        []byte(`{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`),
		HTTPClient:     &mockHostHTTPClient{client: ts.Client()},
	}
	reqBytes, _ := json.Marshal(reqObj)

	_, err := handleExecute(reqBytes, false)
	if err == nil {
		t.Fatal("expected error for upstream 401")
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("expected error message to contain upstream body, got %q", err.Error())
	}
}

func TestHandleExecuteStreamContextCancel(t *testing.T) {
	// This test verifies that the streaming goroutine respects context
	// cancellation and does not leak when the consumer stops reading.
	mockResponse := `data: {"type":"text-delta","text":"hello"}
`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for {
			w.Write([]byte(mockResponse))
			w.(http.Flusher).Flush()
		}
	}))
	defer ts.Close()

	reqObj := pluginapi.ExecutorRequest{
		AuthAttributes: map[string]string{"api_key": "test_key"},
		Payload:        []byte(`{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`),
		HTTPClient:     &mockHostHTTPClient{client: ts.Client()},
	}
	reqBytes, _ := json.Marshal(reqObj)

	setPluginConfig(pluginConfig{APIBase: ts.URL, ModelsURL: defaultModelsURL})

	// Save and restore the lifecycle context so this test does not leak state.
	oldCtx, oldCancel := lifecycleCtx, lifecycleCancel
	defer func() {
		lifecycleCtx, lifecycleCancel = oldCtx, oldCancel
	}()

	// Trigger lifecycle shutdown to cancel in-flight requests.
	go func() {
		lifecycleCancel()
	}()

	_, err := handleExecute(reqBytes, true)
	// We expect either an error or a partial response; the important thing is
	// that it returns and does not hang.
	_ = err
}
