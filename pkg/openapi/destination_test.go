package openapi

import (
	"testing"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/stretchr/testify/require"
)

func TestImportBeforeConfiguringDestination(t *testing.T) {
	for _, serverURL := range []string{"", "/api/v3", "//petstore.swagger.io/v2"} {
		t.Run(serverURL, func(t *testing.T) {
			data := documentWith(t, func(doc map[string]any) {
				delete(doc, "servers")
				if serverURL != "" {
					doc["servers"] = []any{map[string]any{"url": serverURL}}
				}
				doc["components"] = map[string]any{"securitySchemes": map[string]any{
					"token": map[string]any{"type": "http", "scheme": "bearer"},
				}}
			})
			result, err := Parse(data, types.OpenAPIRuntimeConfig{})
			require.NoError(t, err)
			require.Empty(t, result.BaseURL)
			require.NotEmpty(t, result.Schema)
			require.Equal(t, "Users", result.SuggestedMetadata.Name)
			require.Len(t, result.SuggestedHeaders, 1)
			_, err = SettingsJSON(types.OpenAPIRuntimeConfig{}, result, result.SuggestedHeaders)
			require.ErrorContains(t, err, "configure baseURL")
			config := types.OpenAPIRuntimeConfig{Schema: result.Schema}
			_, err = SnapshotSettingsJSON(config, result.SuggestedHeaders)
			require.ErrorContains(t, err, "configure baseURL")
			config.BaseURL = "https://api.example.com/v3"
			_, err = SnapshotSettingsJSON(config, result.SuggestedHeaders)
			require.NoError(t, err)
			config.BaseURL = "http://api.example.com/v3"
			_, err = SnapshotSettingsJSON(config, result.SuggestedHeaders)
			require.ErrorContains(t, err, "HTTPS")
		})
	}
}
