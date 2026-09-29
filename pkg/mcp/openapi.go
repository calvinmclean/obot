package mcp

import (
	"fmt"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/openapi"
)

func configureOpenAPIRuntime(server *ServerConfig, config *types.OpenAPIRuntimeConfig, headers []types.MCPConfig, credentials map[string]string) ([]string, error) {
	if config == nil {
		return nil, fmt.Errorf("openapi runtime requires OpenAPI config")
	}

	env, err := openapi.SnapshotEnvironment(*config, headers)
	if err != nil {
		return nil, err
	}

	server.ContainerPort = 8080
	server.ContainerPath = "/mcp"
	server.HealthzPath = "/healthz"
	server.Env = env
	server.Files = []File{{
		EnvKey:  "OPENAPI_SPEC_FILE",
		Data:    string(config.Schema.Raw),
		Dynamic: false,
	}}

	return configureHeaders(server, headers, credentials), nil
}
