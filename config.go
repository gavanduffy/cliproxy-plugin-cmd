package main

import (
	"net/http"
	"os"
	"sync"
	"time"
)

// pluginConfig holds runtime configuration for the Command Code plugin.
type pluginConfig struct {
	APIBase   string
	ModelsURL string
}

var (
	currentConfig     pluginConfig
	currentConfigLock sync.RWMutex
)

func init() {
	currentConfig = pluginConfig{
		APIBase:   getEnv("COMMANDCODE_API_BASE", "https://api.commandcode.ai"),
		ModelsURL: getEnv("COMMANDCODE_MODELS_URL", "https://api.commandcode.ai/provider/v1/models"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func setPluginConfig(cfg pluginConfig) {
	currentConfigLock.Lock()
	defer currentConfigLock.Unlock()
	currentConfig = cfg
}

func getPluginConfig() pluginConfig {
	currentConfigLock.RLock()
	defer currentConfigLock.RUnlock()
	return currentConfig
}

// newHTTPClient returns a shared HTTP client with reasonable timeouts for
// upstream API calls. The timeout is long enough for chat completions but
// short enough to surface connectivity issues.
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 120 * time.Second,
	}
}
