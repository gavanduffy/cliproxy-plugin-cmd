package main

import (
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// plugin registration types matching the simple plugin example.

type pluginRegistration struct {
	SchemaVersion uint32                 `json:"schema_version"`
	Metadata      pluginapi.Metadata     `json:"metadata"`
	Capabilities  registrationCapability `json:"capabilities"`
}

type registrationCapability struct {
	ModelProvider           bool                         `json:"model_provider"`
	AuthProvider            bool                         `json:"auth_provider"`
	Executor                bool                         `json:"executor"`
	ExecutorModelScope      pluginapi.ExecutorModelScope `json:"executor_model_scope"`
	ExecutorInputFormats    []string                     `json:"executor_input_formats,omitempty"`
	ExecutorOutputFormats   []string                     `json:"executor_output_formats,omitempty"`
}

type identifierResponse struct {
	Identifier string `json:"identifier"`
}

// Command Code API types.

type ccModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextLength int64  `json:"context_length"`
}

type ccModelsResponse struct {
	Object string    `json:"object"`
	Data   []ccModel `json:"data"`
}

type ccMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type ccGenerateParams struct {
	Model    string      `json:"model"`
	Messages []ccMessage `json:"messages"`
	Tools    []any       `json:"tools,omitempty"`
	System   string      `json:"system,omitempty"`
	MaxTokens int        `json:"max_tokens,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
	Stream   bool        `json:"stream"`
}

type ccConfig struct {
	WorkingDir string `json:"workingDir"`
	Date       string `json:"date"`
	Environment string `json:"environment"`
}

type ccGenerateRequest struct {
	Config   ccConfig       `json:"config"`
	Memory   any            `json:"memory,omitempty"`
	Taste    any            `json:"taste,omitempty"`
	Skills   any            `json:"skills,omitempty"`
	Params   ccGenerateParams `json:"params"`
	ThreadID string         `json:"threadId"`
}

type ccStreamEvent struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	ToolCallID string `json:"toolCallId,omitempty"`
	ToolName   string `json:"toolName,omitempty"`
	Input      any    `json:"input,omitempty"`
	Args       any    `json:"args,omitempty"`
	Arguments  any    `json:"arguments,omitempty"`
	FinishReason any  `json:"finishReason,omitempty"`
	Error      any    `json:"error,omitempty"`
}

// OpenAI chat completion types used for input/output.

type openAIMessage struct {
	Role       string `json:"role"`
	Content    any    `json:"content,omitempty"`
	ToolCalls  []any  `json:"tool_calls,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

type openAIChatCompletionRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Stream      bool            `json:"stream,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	Tools       []any           `json:"tools,omitempty"`
}

type openAIDelta struct {
	Role      string `json:"role,omitempty"`
	Content   string `json:"content,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
	ToolCalls []any  `json:"tool_calls,omitempty"`
}

type openAIChoice struct {
	Index        int         `json:"index"`
	Delta        openAIDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason,omitempty"`
}

type openAIChatCompletionChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
}

type openAIChatCompletion struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
}

// streamResponse is the JSON shape returned by the executor for streaming
// requests. It mirrors pluginapi.ExecutorStreamResponse but uses a slice
// instead of a channel so it can be marshaled by the C ABI plugin.
type streamResponse struct {
	Headers http.Header                     `json:"headers"`
	Chunks  []pluginapi.ExecutorStreamChunk `json:"chunks,omitempty"`
}
