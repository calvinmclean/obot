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
	settings, err := openapi.SnapshotSettingsJSON(*config, headers)
	if err != nil {
		return nil, err
	}
	server.ContainerPort = 8080
	server.ContainerPath = "/mcp"
	server.HealthzPath = "/healthz"
	server.Env = []string{"OPENAPI_CONFIG_JSON=" + string(settings)}
	server.Files = []File{{
		EnvKey:  "OPENAPI_SPEC_FILE",
		Data:    string(config.Schema),
		Dynamic: false,
	}}
	return configureHeaders(server, headers, credentials), nil
}
