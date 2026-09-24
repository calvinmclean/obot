package mcp

import (
	"context"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/openapi"
)

// OpenAPIValidator validates saved snapshots without fetching their source.
type OpenAPIValidator struct{}

func (OpenAPIValidator) ValidateConfig(_ context.Context, manifest types.MCPServerManifest) error {
	return validateOpenAPIConfig(manifest.OpenAPIConfig, manifest.Config)
}

func (OpenAPIValidator) ValidateCatalogConfig(_ context.Context, manifest types.MCPServerCatalogEntryManifest) error {
	return validateOpenAPIConfig(manifest.OpenAPIConfig, manifest.Config)
}

func (OpenAPIValidator) ValidateSystemConfig(_ context.Context, _ types.SystemMCPServerManifest) error {
	return types.RuntimeValidationError{
		Runtime: types.RuntimeOpenAPI,
		Field:   "runtime",
		Message: "OpenAPI is not supported for system servers",
	}
}

func validateOpenAPIConfig(config *types.OpenAPIRuntimeConfig, headers []types.MCPConfig) error {
	if config == nil {
		return types.RuntimeValidationError{
			Runtime: types.RuntimeOpenAPI,
			Field:   "openAPIConfig",
			Message: "OpenAPI configuration is required",
		}
	}
	result, err := openapi.Parse(config.Schema, *config)
	if err == nil {
		_, err = openapi.SettingsJSON(*config, result, headers)
	}
	if err != nil {
		return types.RuntimeValidationError{
			Runtime: types.RuntimeOpenAPI,
			Field:   "openAPIConfig",
			Message: err.Error(),
		}
	}
	return nil
}
