package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/api"
	"github.com/obot-platform/obot/pkg/mcp"
	"github.com/obot-platform/obot/pkg/openapi"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/system"
	"github.com/stretchr/testify/require"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const apiOpenAPISchema = `{"openapi":"3.1.0","info":{"title":"Example","version":"1"},"servers":[{"url":"https://api.example.com"}],"paths":{}}`

func newOpenAPIHandler() *MCPCatalogHandler {
	return NewMCPCatalogHandler("", "", "docker", &mcp.SessionManager{}, nil, nil, nil, "")
}

func TestOpenAPIImportRequestErrorDetails(t *testing.T) {
	storage := newFakeStorage(t, &v1.MCPCatalog{Name: "scope", Namespace: system.DefaultNamespace})
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "empty",
			want: "request body is empty",
		},
		{
			name: "malformed JSON",
			body: `{"source":`,
			want: "JSON at byte",
		},
		{
			name: "content object",
			body: `{"source":{"content":{"secret-marker":true}}}`,
			want: `field "source.content": expected string`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := openAPIRequest(t, storage, "catalog_id", http.MethodPost, nil)
			ctx.Request.Body = io.NopCloser(strings.NewReader(test.body))
			err := newOpenAPIHandler().ImportOpenAPI(ctx)
			require.ErrorContains(t, err, test.want)
			require.NotContains(t, err.Error(), "secret-marker")
		})
	}
}

func openAPIRequest(t *testing.T, storage kclient.WithWatch, scope, method string, body any) (api.Context, *httptest.ResponseRecorder) {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	request := httptest.NewRequest(method, "/", bytes.NewReader(data))
	request.SetPathValue(scope, "scope")
	recorder := httptest.NewRecorder()
	return api.Context{
		Request:        request,
		ResponseWriter: recorder,
		Storage:        storage,
		User:           testUserWithRole("admin", types.GroupAdmin),
	}, recorder
}

func TestOpenAPIImportDoesNotCreateEntry(t *testing.T) {
	for _, scope := range []string{"catalog_id", "workspace_id"} {
		t.Run(scope, func(t *testing.T) {
			storage := newFakeStorage(t,
				&v1.MCPCatalog{Name: "scope", Namespace: system.DefaultNamespace},
				&v1.PowerUserWorkspace{Name: "scope", Namespace: system.DefaultNamespace},
			)
			config := types.OpenAPIRuntimeConfig{
				Source: types.OpenAPISource{Content: `
openapi: 3.1.0
info:
  title: Example
  version: "1"
servers:
  - url: /relative
paths: {}
components:
  securitySchemes:
    token:
      type: http
      scheme: bearer
security:
  - token: []
`},
				BaseURL: "https://api.example.com/v1",
			}
			ctx, response := openAPIRequest(t, storage, scope, http.MethodPost, config)
			require.NoError(t, newOpenAPIHandler().ImportOpenAPI(ctx))
			var result types.OpenAPIImportResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
			require.True(t, json.Valid(result.Schema))
			require.Equal(t, "Example", result.SuggestedMetadata.Name)
			require.Equal(t, "https://api.example.com/v1/", result.BaseURL)
			require.Len(t, result.SuggestedHeaders, 1)
			require.Equal(t, "Authorization", result.SuggestedHeaders[0].Key)
			require.Equal(t, "Bearer ", result.SuggestedHeaders[0].Prefix)
			require.Empty(t, result.SuggestedHeaders[0].Value)
			var entries v1.MCPServerCatalogEntryList
			require.NoError(t, storage.List(t.Context(), &entries))
			require.Empty(t, entries.Items)
		})
	}
}

func TestOpenAPIImportRejectsInvalidInput(t *testing.T) {
	storage := newFakeStorage(t, &v1.MCPCatalog{Name: "scope", Namespace: system.DefaultNamespace})
	for _, test := range []struct {
		name   string
		config types.OpenAPIRuntimeConfig
		want   string
	}{
		{
			name: "no source",
			want: "exactly one",
		},
		{
			name: "two sources",
			config: types.OpenAPIRuntimeConfig{Source: types.OpenAPISource{
				URL:     "https://example.com/schema",
				Content: apiOpenAPISchema,
			}},
			want: "exactly one",
		},
		{
			name:   "invalid schema",
			config: types.OpenAPIRuntimeConfig{Source: types.OpenAPISource{Content: "secret-invalid-schema"}},
			want:   "schema",
		},
		{
			name:   "oversize schema",
			config: types.OpenAPIRuntimeConfig{Source: types.OpenAPISource{Content: strings.Repeat(" ", openapi.MaxSchemaBytes+1)}},
			want:   "1 MiB",
		},
		{
			name:   "private source",
			config: types.OpenAPIRuntimeConfig{Source: types.OpenAPISource{URL: "http://127.0.0.1/schema"}},
			want:   "schema fetch failed",
		},
		{
			name: "exclusion without search",
			config: types.OpenAPIRuntimeConfig{
				Source:  types.OpenAPISource{Content: apiOpenAPISchema},
				Exclude: []types.OpenAPIExclusion{{Method: "post"}},
			},
			want: "toolSearch=true",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := openAPIRequest(t, storage, "catalog_id", http.MethodPost, test.config)
			err := newOpenAPIHandler().ImportOpenAPI(ctx)
			require.ErrorContains(t, err, test.want)
			require.NotContains(t, err.Error(), "secret-invalid-schema")
		})
	}
}

func TestOpenAPICatalogCreateAndUpdate(t *testing.T) {
	for _, scope := range []string{"catalog_id", "workspace_id"} {
		t.Run(scope, func(t *testing.T) {
			var fetches atomic.Int64
			var schema atomic.Value
			schema.Store(apiOpenAPISchema)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fetches.Add(1)
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Error("request credentials were forwarded to the schema source")
				}
				_, _ = w.Write([]byte(schema.Load().(string)))
			}))
			defer upstream.Close()
			storage := newFakeStorage(t,
				&v1.MCPCatalog{Name: "scope", Namespace: system.DefaultNamespace},
				&v1.PowerUserWorkspace{Name: "scope", Namespace: system.DefaultNamespace},
			)
			handler := newOpenAPIHandler()
			handler.openAPIImporter = openapi.NewImporter([]string{"127.0.0.1"})
			manifest := types.MCPServerCatalogEntryManifest{
				Name:    "Example",
				Runtime: types.RuntimeOpenAPI,
				OpenAPIConfig: &types.OpenAPIRuntimeConfig{
					Source: types.OpenAPISource{URL: upstream.URL},
				},
			}
			ctx, _ := openAPIRequest(t, storage, scope, http.MethodPost, manifest)
			ctx.Request.Header.Set("Authorization", "Bearer user-secret")
			ctx.Request.Header.Set("Cookie", "session=user-secret")
			require.NoError(t, handler.CreateEntry(ctx))
			require.EqualValues(t, 1, fetches.Load())
			var entries v1.MCPServerCatalogEntryList
			require.NoError(t, storage.List(t.Context(), &entries))
			require.Len(t, entries.Items, 1)
			entry := entries.Items[0]
			require.JSONEq(t, apiOpenAPISchema, string(entry.Spec.Manifest.OpenAPIConfig.Schema))
			deployedManifest, err := types.MapCatalogEntryToServer(entry.Spec.Manifest, "", false)
			require.NoError(t, err)
			deployed := &v1.MCPServer{
				Name:      "deployed",
				Namespace: system.DefaultNamespace,
				Spec: v1.MCPServerSpec{
					MCPServerCatalogEntryName: entry.Name,
					Manifest:                  deployedManifest,
				},
			}
			require.NoError(t, storage.Create(t.Context(), deployed))

			// Updating ordinary settings keeps the deployed snapshot, even when
			// the remote source is now invalid.
			schema.Store("not a schema")
			manifest.OpenAPIConfig.BaseURL = "https://override.example.com"
			ctx, _ = openAPIRequest(t, storage, scope, http.MethodPut, manifest)
			ctx.Request.SetPathValue("entry_id", entry.Name)
			require.NoError(t, handler.UpdateEntry(ctx))
			require.EqualValues(t, 1, fetches.Load())

			// A changed source with no snapshot is imported; failure must not
			// overwrite either the old schema or the settings.
			manifest.OpenAPIConfig.Source.URL = upstream.URL + "/changed"
			ctx, _ = openAPIRequest(t, storage, scope, http.MethodPut, manifest)
			ctx.Request.SetPathValue("entry_id", entry.Name)
			require.Error(t, handler.UpdateEntry(ctx))
			require.NoError(t, storage.Get(t.Context(), kclient.ObjectKeyFromObject(&entry), &entry))
			require.Equal(t, upstream.URL, entry.Spec.Manifest.OpenAPIConfig.Source.URL)
			require.Equal(t, "https://override.example.com", entry.Spec.Manifest.OpenAPIConfig.BaseURL)
			require.JSONEq(t, apiOpenAPISchema, string(entry.Spec.Manifest.OpenAPIConfig.Schema))

			// Explicitly providing a new validated snapshot updates the entry.
			manifest.OpenAPIConfig.Schema = json.RawMessage(strings.Replace(apiOpenAPISchema, "Example", "Updated", 1))
			ctx, _ = openAPIRequest(t, storage, scope, http.MethodPut, manifest)
			ctx.Request.SetPathValue("entry_id", entry.Name)
			require.NoError(t, handler.UpdateEntry(ctx))
			require.EqualValues(t, 2, fetches.Load())
			require.NoError(t, storage.Get(t.Context(), kclient.ObjectKeyFromObject(&entry), &entry))
			require.Contains(t, string(entry.Spec.Manifest.OpenAPIConfig.Schema), "Updated")
			require.NoError(t, storage.Get(t.Context(), kclient.ObjectKeyFromObject(deployed), deployed))
			require.JSONEq(t, apiOpenAPISchema, string(deployed.Spec.Manifest.OpenAPIConfig.Schema))
			require.Empty(t, deployed.Spec.Manifest.OpenAPIConfig.BaseURL, "catalog edits do not upgrade a server")
		})
	}
}

func TestOpenAPICatalogRejectsInvalidConfiguration(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*types.MCPServerCatalogEntryManifest)
		want   string
	}{
		{
			name: "exclusions require search",
			mutate: func(m *types.MCPServerCatalogEntryManifest) {
				m.OpenAPIConfig.Exclude = []types.OpenAPIExclusion{{Method: "GET"}}
			},
			want: "toolSearch=true",
		},
		{
			name: "credentials must be headers",
			mutate: func(m *types.MCPServerCatalogEntryManifest) {
				m.Config = []types.MCPConfig{{Key: "TOKEN", Usage: types.Env, Required: true, Sensitive: true}}
			},
			want: "header inputs",
		},
		{
			name: "snapshot does not waive source validation",
			mutate: func(m *types.MCPServerCatalogEntryManifest) {
				m.OpenAPIConfig.Source = types.OpenAPISource{URL: "file:///etc/passwd"}
				m.OpenAPIConfig.Schema = json.RawMessage(apiOpenAPISchema)
			},
			want: "absolute HTTP(S)",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			storage := newFakeStorage(t, &v1.MCPCatalog{Name: "scope", Namespace: system.DefaultNamespace})
			manifest := types.MCPServerCatalogEntryManifest{
				Name:    "Example",
				Runtime: types.RuntimeOpenAPI,
				OpenAPIConfig: &types.OpenAPIRuntimeConfig{
					Source: types.OpenAPISource{Content: apiOpenAPISchema},
				},
			}
			test.mutate(&manifest)
			ctx, _ := openAPIRequest(t, storage, "catalog_id", http.MethodPost, manifest)
			require.ErrorContains(t, newOpenAPIHandler().CreateEntry(ctx), test.want)
			var entries v1.MCPServerCatalogEntryList
			require.NoError(t, storage.List(t.Context(), &entries))
			require.Empty(t, entries.Items)
		})
	}
}

func TestOpenAPICatalogAllowsRemovingSuggestedHeader(t *testing.T) {
	storage := newFakeStorage(t, &v1.MCPCatalog{Name: "scope", Namespace: system.DefaultNamespace})
	manifest := types.MCPServerCatalogEntryManifest{
		Name:    "Example",
		Runtime: types.RuntimeOpenAPI,
		Config:  []types.MCPConfig{{Key: "X-API-Key", Usage: types.Header}},
		OpenAPIConfig: &types.OpenAPIRuntimeConfig{
			Source: types.OpenAPISource{Content: strings.Replace(apiOpenAPISchema, `"paths":{}`, `"paths":{},"components":{"securitySchemes":{"key":{"type":"apiKey","in":"header","name":"X-API-Key"}}},"security":[{"key":[]}]`, 1)},
		},
	}
	handler := newOpenAPIHandler()
	ctx, _ := openAPIRequest(t, storage, "catalog_id", http.MethodPost, manifest)
	require.NoError(t, handler.CreateEntry(ctx))
	var entries v1.MCPServerCatalogEntryList
	require.NoError(t, storage.List(t.Context(), &entries))
	require.Len(t, entries.Items, 1)
	manifest.Config = nil
	ctx, _ = openAPIRequest(t, storage, "catalog_id", http.MethodPut, manifest)
	ctx.Request.SetPathValue("entry_id", entries.Items[0].Name)
	require.NoError(t, handler.UpdateEntry(ctx))
	require.NoError(t, storage.List(t.Context(), &entries))
	require.Empty(t, entries.Items[0].Spec.Manifest.Config)
}

func TestOpenAPIUpdateRespectsOwnership(t *testing.T) {
	for _, test := range []struct {
		name     string
		catalog  string
		editable bool
		want     string
	}{
		{
			name:     "different catalog",
			catalog:  "other",
			editable: true,
			want:     "does not belong to catalog",
		},
		{
			name:    "git managed",
			catalog: "scope",
			want:    "not editable",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			entry := &v1.MCPServerCatalogEntry{
				Name:      "entry",
				Namespace: system.DefaultNamespace,
				Spec: v1.MCPServerCatalogEntrySpec{
					Editable:       test.editable,
					MCPCatalogName: test.catalog,
				},
			}
			storage := newFakeStorage(t, entry, &v1.MCPCatalog{Name: "scope", Namespace: system.DefaultNamespace})
			ctx, _ := openAPIRequest(t, storage, "catalog_id", http.MethodPut, types.MCPServerCatalogEntryManifest{
				Runtime: types.RuntimeOpenAPI,
				OpenAPIConfig: &types.OpenAPIRuntimeConfig{
					Source: types.OpenAPISource{Content: apiOpenAPISchema},
				},
			})
			ctx.Request.SetPathValue("entry_id", entry.Name)
			require.ErrorContains(t, newOpenAPIHandler().UpdateEntry(ctx), test.want)
		})
	}
}

func TestOpenAPISaveImportedSnapshot(t *testing.T) {
	storage := newFakeStorage(t, &v1.MCPCatalog{Name: "scope", Namespace: system.DefaultNamespace})
	handler := newOpenAPIHandler()
	config := types.OpenAPIRuntimeConfig{Source: types.OpenAPISource{Content: apiOpenAPISchema}}
	ctx, response := openAPIRequest(t, storage, "catalog_id", http.MethodPost, config)
	require.NoError(t, handler.ImportOpenAPI(ctx))
	var imported types.OpenAPIImportResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &imported))
	config.Source = types.OpenAPISource{URL: "https://unreachable.invalid/schema"}
	config.Schema = imported.Schema
	manifest := types.MCPServerCatalogEntryManifest{
		Name:          "Imported",
		Runtime:       types.RuntimeOpenAPI,
		OpenAPIConfig: &config,
	}
	ctx, _ = openAPIRequest(t, storage, "catalog_id", http.MethodPost, manifest)
	require.NoError(t, handler.CreateEntry(ctx), "saving an imported snapshot must not refetch its source")

	config.Schema = json.RawMessage(`{"invalid":true}`)
	ctx, _ = openAPIRequest(t, storage, "catalog_id", http.MethodPost, manifest)
	require.Error(t, handler.CreateEntry(ctx), "client-supplied snapshots still need validation")
	var entries v1.MCPServerCatalogEntryList
	require.NoError(t, storage.List(t.Context(), &entries))
	require.Len(t, entries.Items, 1)
}
