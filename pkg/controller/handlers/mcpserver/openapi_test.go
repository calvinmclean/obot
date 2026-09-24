package mcpserver

import (
	"encoding/json"
	"testing"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/stretchr/testify/require"
)

func TestOpenAPIConfigurationDrift(t *testing.T) {
	base := &types.OpenAPIRuntimeConfig{
		Source: types.OpenAPISource{URL: "https://example.com/openapi.json"},
		Schema: json.RawMessage(`{"openapi":"3.1.0"}`),
	}
	for _, test := range []struct {
		name   string
		mutate func(*types.OpenAPIRuntimeConfig)
		want   bool
	}{
		{
			name: "source URL only",
			mutate: func(c *types.OpenAPIRuntimeConfig) {
				c.Source.URL = "https://other.example.com/schema"
			},
		},
		{
			name: "source changed to inline",
			mutate: func(c *types.OpenAPIRuntimeConfig) {
				c.Source = types.OpenAPISource{Content: string(c.Schema)}
			},
		},
		{
			name: "snapshot formatting",
			mutate: func(c *types.OpenAPIRuntimeConfig) {
				c.Schema = json.RawMessage(`{ "openapi": "3.1.0" }`)
			},
		},
		{
			name: "schema",
			mutate: func(c *types.OpenAPIRuntimeConfig) {
				c.Schema = json.RawMessage(`{"openapi":"3.1.1"}`)
			},
			want: true,
		},
		{
			name: "base URL",
			mutate: func(c *types.OpenAPIRuntimeConfig) {
				c.BaseURL = "https://api.example.com"
			},
			want: true,
		},
		{
			name: "tool search",
			mutate: func(c *types.OpenAPIRuntimeConfig) {
				c.ToolSearch = true
			},
			want: true,
		},
		{
			name: "exclusion",
			mutate: func(c *types.OpenAPIRuntimeConfig) {
				c.Exclude = []types.OpenAPIExclusion{{Method: "POST", PathPattern: "^/users$"}}
			},
			want: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			entry := base.DeepCopy()
			test.mutate(entry)
			drifted, err := configurationHasDrifted(
				types.MCPServerManifest{Runtime: types.RuntimeOpenAPI, OpenAPIConfig: base},
				types.MCPServerCatalogEntryManifest{Runtime: types.RuntimeOpenAPI, OpenAPIConfig: entry},
				false,
			)
			require.NoError(t, err)
			require.Equal(t, test.want, drifted)
		})
	}
}

func TestOpenAPIExclusionCaseDoesNotDrift(t *testing.T) {
	server := &types.OpenAPIRuntimeConfig{
		ToolSearch: true,
		Exclude: []types.OpenAPIExclusion{{
			Method:      "POST",
			PathPattern: "^/users$",
			Tag:         "internal",
		}},
	}
	entry := server.DeepCopy()
	entry.Exclude[0].Method = "pOsT"
	require.False(t, openAPIConfigHasDrifted(server, entry))
	entry.Exclude[0].Tag = "Internal"
	require.True(t, openAPIConfigHasDrifted(server, entry), "tags remain exact matches")
}
