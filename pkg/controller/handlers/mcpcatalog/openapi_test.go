package mcpcatalog

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/obot-platform/nah/pkg/router"
	"github.com/obot-platform/obot/apiclient/types"
	serverhandler "github.com/obot-platform/obot/pkg/controller/handlers/mcpserver"
	"github.com/obot-platform/obot/pkg/mcp"
	"github.com/obot-platform/obot/pkg/openapi"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/stretchr/testify/require"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const catalogOpenAPISchema = `{"openapi":"3.1.0","info":{"title":"Example","version":"1"},"servers":[{"url":"https://api.example.com"}],"paths":{}}`

func writeOpenAPICatalog(t *testing.T, dir string, source types.OpenAPISource) string {
	t.Helper()
	path := filepath.Join(dir, "openapi.json")
	entry := types.MCPServerCatalogEntryManifest{
		Name:             "Example API",
		Description:      "Example API",
		ShortDescription: "Example API",
		Runtime:          types.RuntimeOpenAPI,
		OpenAPIConfig: &types.OpenAPIRuntimeConfig{
			Source: source,
			// A Git-supplied snapshot must never take precedence over the source.
			Schema: json.RawMessage(`{"ignored":true}`),
		},
	}
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func TestOpenAPICatalogSyncLifecycle(t *testing.T) {
	// The catalog and info.version stay unchanged while the URL's content changes.
	schema := catalogOpenAPISchema
	status := http.StatusOK
	requests := 0
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Empty(t, r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("If-None-Match"))
		require.Empty(t, r.Header.Get("If-Modified-Since"))
		w.Header().Set("ETag", "unchanged")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(schema))
	}))
	defer source.Close()
	dir := t.TempDir()
	writeOpenAPICatalog(t, dir, types.OpenAPISource{URL: source.URL})
	catalog := testCatalog()
	catalog.Spec.SourceURLs = []string{dir}
	client := newCatalogFakeClient(catalog)
	handler := newParseTestHandler(t)
	handler.openAPIImporter = openapi.NewImporter([]string{"127.0.0.1"})

	syncCatalog := func() *v1.MCPCatalog {
		t.Helper()
		current := &v1.MCPCatalog{}
		require.NoError(t, client.Get(t.Context(), kclient.ObjectKeyFromObject(catalog), current))
		if current.Annotations == nil {
			current.Annotations = map[string]string{}
		}
		current.Annotations[v1.MCPCatalogSyncAnnotation] = "true"
		require.NoError(t, client.Update(t.Context(), current))
		require.NoError(t, handler.Sync(router.Request{Ctx: t.Context(), Client: client, Object: current}, &parseTestResponse{}))
		require.NoError(t, client.Get(t.Context(), kclient.ObjectKeyFromObject(catalog), current))
		return current
	}
	getEntry := func() v1.MCPServerCatalogEntry {
		t.Helper()
		var entries v1.MCPServerCatalogEntryList
		require.NoError(t, client.List(t.Context(), &entries))
		for _, entry := range entries.Items {
			if entry.Spec.Manifest.Runtime == types.RuntimeOpenAPI {
				return entry
			}
		}
		t.Fatal("OpenAPI entry is missing")
		return v1.MCPServerCatalogEntry{}
	}

	require.Empty(t, syncCatalog().Status.SyncErrors)
	entry := getEntry()
	require.JSONEq(t, catalogOpenAPISchema, string(entry.Spec.Manifest.OpenAPIConfig.Schema))
	manifest, err := types.MapCatalogEntryToServer(entry.Spec.Manifest, "", false)
	require.NoError(t, err)
	server := &v1.MCPServer{
		Name:      "deployed-api",
		Namespace: catalog.Namespace,
		Spec: v1.MCPServerSpec{
			MCPServerCatalogEntryName: entry.Name,
			Manifest:                  manifest,
		},
	}
	require.NoError(t, client.Create(t.Context(), server))
	deployedSnapshot := string(manifest.OpenAPIConfig.Schema)

	schema = strings.Replace(catalogOpenAPISchema, "Example", "Updated", 1)
	require.Empty(t, syncCatalog().Status.SyncErrors)
	require.Equal(t, 2, requests)
	entry = getEntry()
	require.JSONEq(t, schema, string(entry.Spec.Manifest.OpenAPIConfig.Schema))
	var running v1.MCPServer
	require.NoError(t, client.Get(t.Context(), kclient.ObjectKeyFromObject(server), &running))
	require.Equal(t, deployedSnapshot, string(running.Spec.Manifest.OpenAPIConfig.Schema))
	drifted, err := serverhandler.ConfigurationHasDrifted(t.Context(), nil, &running, entry.Spec.Manifest, false)
	require.NoError(t, err)
	require.True(t, drifted)

	// Formatting alone produces the same saved snapshot.
	lastGood := string(entry.Spec.Manifest.OpenAPIConfig.Schema)
	schema = " \n" + schema + "\n "
	require.Empty(t, syncCatalog().Status.SyncErrors)
	require.Equal(t, lastGood, string(getEntry().Spec.Manifest.OpenAPIConfig.Schema))

	// Failures preserve the old entry; other valid entries still sync.
	writeParseTestManifest(t, dir, "Sibling", "healthy")
	status = http.StatusServiceUnavailable
	require.Contains(t, syncCatalog().Status.SyncErrors[dir], "HTTP 503")
	require.Equal(t, lastGood, string(getEntry().Spec.Manifest.OpenAPIConfig.Schema))
	var entries v1.MCPServerCatalogEntryList
	require.NoError(t, client.List(t.Context(), &entries))
	require.Len(t, entries.Items, 2)

	status = http.StatusOK
	schema = "not a schema"
	require.NotEmpty(t, syncCatalog().Status.SyncErrors[dir])
	require.Equal(t, lastGood, string(getEntry().Spec.Manifest.OpenAPIConfig.Schema))

	schema = catalogOpenAPISchema
	require.Empty(t, syncCatalog().Status.SyncErrors)
	entry = getEntry()
	drifted, err = serverhandler.ConfigurationHasDrifted(t.Context(), nil, &running, entry.Spec.Manifest, false)
	require.NoError(t, err)
	require.False(t, drifted, "restoring the deployed schema clears drift")
	require.Equal(t, 6, requests, "every sync fetches the schema")
}

func TestOpenAPICatalogInlineSchema(t *testing.T) {
	dir := t.TempDir()
	writeOpenAPICatalog(t, dir, types.OpenAPISource{Content: catalogOpenAPISchema})
	handler := New("", "", nil, nil, &mcp.SessionManager{}, 0)
	require.NotNil(t, handler.openAPIImporter)
	entries, err := handler.readMCPCatalog(t.Context(), "default", dir, "")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.JSONEq(t, catalogOpenAPISchema, string(entries[0].(*v1.MCPServerCatalogEntry).Spec.Manifest.OpenAPIConfig.Schema))
}

func TestOpenAPICatalogSchemaFetchIsIsolated(t *testing.T) {
	var authorization string
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(catalogOpenAPISchema))
	}))
	defer source.Close()
	dir := t.TempDir()
	writeOpenAPICatalog(t, dir, types.OpenAPISource{URL: source.URL})
	handler := &Handler{openAPIImporter: openapi.NewImporter([]string{"127.0.0.1"})}
	entries, err := handler.readMCPCatalog(t.Context(), "default", dir, "catalog-secret")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Empty(t, authorization, "catalog credentials must not reach the schema host")

	handler = New("", "", nil, nil, &mcp.SessionManager{}, 0)
	entries, err = handler.readMCPCatalog(t.Context(), "default", dir, "")
	require.ErrorContains(t, err, "schema fetch failed")
	require.Empty(t, entries, "the production importer blocks loopback destinations")
}
