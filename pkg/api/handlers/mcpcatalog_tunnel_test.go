package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	storagescheme "github.com/obot-platform/obot/pkg/storage/scheme"
	"github.com/obot-platform/obot/pkg/system"
	"github.com/stretchr/testify/require"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSyncedCatalogTunnelAssignment(t *testing.T) {
	const (
		source = "https://example.com/catalog"
		target = "https://inventory.example/mcp"
	)
	catalog := &v1.MCPCatalog{
		Name:      "catalog",
		Namespace: system.DefaultNamespace,
		Spec: v1.MCPCatalogSpec{
			SourceURLs: []string{source},
		},
	}
	entry := &v1.MCPServerCatalogEntry{
		Name:      "inventory-entry",
		Namespace: system.DefaultNamespace,
		Spec: v1.MCPServerCatalogEntrySpec{
			MCPCatalogName: catalog.Name,
			SourceURL:      source,
			Manifest: types.MCPServerCatalogEntryManifest{
				EntryKey: "inventory",
				Runtime:  types.RuntimeRemote,
				RemoteConfig: &types.RemoteCatalogConfig{
					FixedURL: target,
				},
			},
		},
	}
	configuredTunnel := &v1.MCPTunnel{
		Name:      "mt1office",
		Namespace: system.DefaultNamespace,
		Spec: v1.MCPTunnelSpec{
			Manifest: types.MCPTunnelManifest{
				DisplayName: "Office",
				AllowedURLs: []string{target},
			},
		},
	}
	storage := fake.NewClientBuilder().WithScheme(storagescheme.Scheme).
		WithIndex(entry, "spec.mcpCatalogName", func(obj kclient.Object) []string {
			return []string{obj.(*v1.MCPServerCatalogEntry).Spec.MCPCatalogName}
		}).
		WithObjects(catalog, entry, configuredTunnel).Build()
	handler := &MCPCatalogHandler{}
	path := "/api/mcp-catalogs/catalog/entries/by-key/inventory/tunnel?sourceURL=" + url.QueryEscape(source)
	values := map[string]string{"catalog_id": "catalog", "entry_key": "inventory"}

	req, _ := catalogEntryRequest(t, storage, nil, http.MethodGet, path, nil, values)
	require.ErrorContains(t, handler.GetTunnelAssignment(req), "tunnel assignment not found")
	req.User = testUserWithRole("auditor", types.GroupAuditor)
	require.ErrorContains(t, handler.GetTunnelAssignment(req), "require an administrator")

	input := catalogTunnelAssignmentRequest{TunnelName: configuredTunnel.Name, ExpectedURL: target}
	stale := input
	stale.ExpectedURL += "/elsewhere"
	req, _ = catalogEntryRequest(t, storage, nil, http.MethodPut, path, stale, values)
	require.ErrorContains(t, handler.PutTunnelAssignment(req), "must match the current Git")

	req, _ = catalogEntryRequest(t, storage, nil, http.MethodPut, path, input, values)
	require.NoError(t, handler.PutTunnelAssignment(req))

	require.NoError(t, storage.Get(t.Context(), kclient.ObjectKeyFromObject(entry), entry))
	require.Empty(t, entry.Spec.Manifest.RemoteConfig.TunnelName)
	entry.Spec.Manifest.RemoteConfig.FixedURL += "/changed"
	require.NoError(t, storage.Update(t.Context(), entry))

	req, response := catalogEntryRequest(t, storage, nil, http.MethodGet, path, nil, values)
	require.NoError(t, handler.GetTunnelAssignment(req))
	var state catalogTunnelAssignmentResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &state))
	require.Equal(t, target, state.ExpectedURL)
	require.Equal(t, entry.Spec.Manifest.RemoteConfig.FixedURL, state.CurrentURL)
	require.False(t, state.Valid)

	for range 2 {
		req, response = catalogEntryRequest(t, storage, nil, http.MethodDelete, path, nil, values)
		require.NoError(t, handler.DeleteTunnelAssignment(req))
		require.Equal(t, http.StatusNoContent, response.Code)
	}
	require.NoError(t, storage.Get(t.Context(), kclient.ObjectKeyFromObject(entry), entry))
	require.Nil(t, entry.Spec.TunnelAssignment)

	req, _ = catalogEntryRequest(t, storage, nil, http.MethodPut, path, catalogTunnelAssignmentRequest{
		TunnelName:  configuredTunnel.Name,
		ExpectedURL: target,
	}, values)
	entry.Spec.Manifest.RemoteConfig.FixedURL = target
	require.NoError(t, storage.Update(t.Context(), entry))
	require.NoError(t, handler.PutTunnelAssignment(req))

	require.NoError(t, storage.Get(t.Context(), kclient.ObjectKeyFromObject(catalog), catalog))
	catalog.Spec.SourceURLs = nil
	require.NoError(t, storage.Update(t.Context(), catalog))
	require.NoError(t, clearReaddedSourceTunnelAssignments(req, catalog, []string{source}))
	catalog.Spec.SourceURLs = []string{source}
	require.NoError(t, storage.Update(t.Context(), catalog))
	req, _ = catalogEntryRequest(t, storage, nil, http.MethodGet, path, nil, values)
	require.ErrorContains(t, handler.GetTunnelAssignment(req), "tunnel assignment not found")
}
