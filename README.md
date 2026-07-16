# Command Code Provider for CLIProxyAPI

A CLIProxyAPI dynamic plugin that exposes [Command Code](https://commandcode.ai) models through an OpenAI-compatible chat completions interface.

## Features

- **Model discovery**: Fetches the current model list from `https://api.commandcode.ai/provider/v1/models` at startup.
- **Authentication**: Reads the Command Code API key from auth files or the `COMMANDCODE_API_KEY` environment variable.
- **Executor**: Forwards chat completion requests to `https://api.commandcode.ai/alpha/generate`.
- **Streaming**: Translates Command Code SSE events into OpenAI-compatible SSE chunks.
- **Token counting**: Local token estimation without hitting upstream chat endpoints.
- **Graceful shutdown**: Plugin-level lifecycle context cancels in-flight requests on host shutdown.

## Installation

Build the shared library for Linux:

```bash
make build-linux
```

This produces `commandcode.so` (and `commandcode.h`).

Copy the shared library into your CLIProxyAPI `plugins` directory (default: `./plugins`):

```bash
cp commandcode.so /path/to/cliproxy/plugins/
```

Enable plugins in your `config.yaml`:

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    commandcode:
      enabled: true
      priority: 1
      api_base: "https://api.commandcode.ai"
      models_url: "https://api.commandcode.ai/provider/v1/models"
```

## Authentication

Create `~/.cli-proxy-api/commandcode.json` (or another auth file matched by CLIProxyAPI) with:

```json
{
  "apiKey": "user_..."
}
```

Or set the environment variable:

```bash
export COMMANDCODE_API_KEY="user_..."
```

## Usage

Once the plugin is loaded, models appear in `/v1/models` under the `commandcode` provider. Send chat completion requests as usual:

```bash
curl http://localhost:8317/v1/chat/completions \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "commandcode/deepseek/deepseek-v4-flash",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": true
  }'
```

## Configuration

Plugin config fields (under `plugins.configs.commandcode`):

- `api_base` — override the Command Code API base URL.
- `models_url` — override the models endpoint URL.

Environment variables:

- `COMMANDCODE_API_KEY` — API key fallback.
- `COMMANDCODE_API_BASE` — API base URL fallback.
- `COMMANDCODE_MODELS_URL` — models endpoint URL fallback.

## Development

```bash
# Build for Linux
make build-linux

# Run tests
make test

# Clean artifacts
make clean
```

## License

MIT
