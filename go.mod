module github.com/router-for-me/cliproxy-plugin-commandcode

go 1.26.0

require (
	github.com/google/uuid v1.6.0
	github.com/router-for-me/CLIProxyAPI/v7 v7.0.0
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/router-for-me/CLIProxyAPI/v7 => /tmp/cli-target
