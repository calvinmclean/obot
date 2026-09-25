package mcp

import (
	"encoding/json"
	"testing"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/stretchr/testify/require"
)

func TestOpenAPIManifestValidation(t *testing.T) {
	base := openAPITestServer().Spec.Manifest.ConvertToCatalogEntry()
	for _, test := range []struct {
		name   string
		mutate func(*types.MCPServerCatalogEntryManifest)
		want   string
	}{
		{
			name: "saved snapshot without fetching source",
			mutate: func(m *types.MCPServerCatalogEntryManifest) {
				m.OpenAPIConfig.Source = types.OpenAPISource{URL: "https://unreachable.invalid/schema"}
			},
		},
		{
			name: "missing config",
			mutate: func(m *types.MCPServerCatalogEntryManifest) {
				m.OpenAPIConfig = nil
			},
			want: "OpenAPI configuration is required",
		},
		{
			name: "missing snapshot",
			mutate: func(m *types.MCPServerCatalogEntryManifest) {
				m.OpenAPIConfig.Schema = nil
			},
			want: "schema",
		},
		{
			name: "invalid snapshot",
			mutate: func(m *types.MCPServerCatalogEntryManifest) {
				m.OpenAPIConfig.Schema = json.RawMessage(`{}`)
			},
			want: "OpenAPI",
		},
		{
			name: "exclusion without search",
			mutate: func(m *types.MCPServerCatalogEntryManifest) {
				m.OpenAPIConfig.ToolSearch = false
			},
			want: "toolSearch",
		},
		{
			name: "environment credential",
			mutate: func(m *types.MCPServerCatalogEntryManifest) {
				m.Config[0].Usage = types.Env
			},
			want: "header inputs",
		},
		{
			name: "suggested security header may be omitted",
			mutate: func(m *types.MCPServerCatalogEntryManifest) {
				m.OpenAPIConfig.Schema = json.RawMessage(`{"openapi":"3.1.0","info":{"title":"Test","version":"1"},"servers":[{"url":"https://api.example.com"}],"paths":{},"components":{"securitySchemes":{"key":{"type":"apiKey","in":"header","name":"X-API-Key"}}},"security":[{"key":[]}]}`)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifest := base.DeepCopy()
			test.mutate(manifest)
			err := ValidateCatalogEntryManifest(t.Context(), *manifest, true, ValidationOptions{})
			if test.want != "" {
				require.ErrorContains(t, err, test.want)
				return
			}
			require.NoError(t, err)
			server, err := types.MapCatalogEntryToServer(*manifest, "", false)
			require.NoError(t, err)
			require.NoError(t, ValidateServerManifest(t.Context(), server, false, ValidationOptions{}))
		})
	}
}
