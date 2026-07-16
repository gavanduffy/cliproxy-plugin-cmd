package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const (
	commandCodeVersion = "0.29.0"
)

func handleAuthParse(request []byte) ([]byte, error) {
	var req pluginapi.AuthParseRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, err
	}

	apiKey, err := extractAPIKey(req.RawJSON, req.FileName)
	if err != nil {
		return okEnvelope(pluginapi.AuthParseResponse{Handled: false})
	}

	return okEnvelope(pluginapi.AuthParseResponse{
		Handled: true,
		Auth: pluginapi.AuthData{
			Provider:    pluginName,
			ID:          "commandcode-default",
			FileName:    req.FileName,
			Label:       "Command Code",
			StorageJSON: req.RawJSON,
			Metadata: map[string]any{
				"api_key": apiKey,
			},
			Attributes: map[string]string{
				"api_key": apiKey,
			},
		},
	})
}

func handleAuthLoginStart(request []byte) ([]byte, error) {
	var req pluginapi.AuthLoginStartRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, err
	}

	// Command Code API keys do not expire; browser login is optional.
	// Return a start response that points the user to manual key setup.
	return okEnvelope(pluginapi.AuthLoginStartResponse{
		Provider: pluginName,
		URL:      "https://commandcode.ai/studio",
		State:    "manual",
		ExpiresAt: time.Now().Add(5 * time.Minute),
		Metadata: map[string]any{
			"message": "Command Code plugin requires an API key. Set COMMANDCODE_API_KEY or place the key in ~/.commandcode/auth.json as {\"apiKey\":\"...\"}",
		},
	})
}

func handleAuthLoginPoll(request []byte) ([]byte, error) {
	return okEnvelope(pluginapi.AuthLoginPollResponse{
		Status:  pluginapi.AuthLoginStatusError,
		Message: "Command Code plugin does not support interactive login polling. Provide an API key via auth file or environment variable.",
	})
}

func handleAuthRefresh(request []byte) ([]byte, error) {
	var req pluginapi.AuthRefreshRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, err
	}

	apiKey, err := extractAPIKey(req.StorageJSON, "")
	if err != nil {
		return nil, err
	}

	return okEnvelope(pluginapi.AuthRefreshResponse{
		Auth: pluginapi.AuthData{
			Provider: pluginName,
			ID:       req.AuthID,
			Label:    "Command Code",
			StorageJSON: req.StorageJSON,
			Metadata: map[string]any{
				"api_key": apiKey,
			},
			Attributes: map[string]string{
				"api_key": apiKey,
			},
			NextRefreshAfter: time.Now().Add(365 * 24 * time.Hour),
		},
	})
}

func extractAPIKey(raw []byte, filename string) (string, error) {
	isCommandCodeFile := filename == "commandcode.json" || filename == ".commandcode.json"

	var data map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &data); err != nil {
			return "", err
		}
	} else {
		data = make(map[string]any)
	}

	// Provider-specific object.
	if cc, ok := data["command-code"].(map[string]any); ok {
		if t, _ := cc["type"].(string); t == "api" {
			if v, ok := cc["key"].(string); ok && v != "" {
				return v, nil
			}
		}
		if v, ok := cc["key"].(string); ok && v != "" {
			return v, nil
		}
	}

	// Legacy commandcode field.
	if v, ok := data["commandcode"].(string); ok && v != "" {
		return v, nil
	}

	// Generic keys only when the file/provider is explicitly ours.
	provider, _ := data["provider"].(string)
	if isCommandCodeFile || provider == "commandcode" {
		if v, ok := data["apiKey"].(string); ok && v != "" {
			return v, nil
		}
		if v := os.Getenv("COMMANDCODE_API_KEY"); v != "" {
			return v, nil
		}
	}

	return "", fmt.Errorf("no Command Code API key found")
}
