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
	require.NoError(t, storage.Get(t.Context(), kclient.ObjectKeyFromObject(catalog), catalog))
	requestID := catalog.Annotations[v1.MCPCatalogSyncAnnotation]
	require.NotEmpty(t, requestID)

	req, _ = catalogEntryRequest(t, storage, nil, http.MethodPut, path, input, values)
	require.NoError(t, handler.PutTunnelAssignment(req))
	require.NoError(t, storage.Get(t.Context(), kclient.ObjectKeyFromObject(catalog), catalog))
	require.NotEqual(t, requestID, catalog.Annotations[v1.MCPCatalogSyncAnnotation])

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
		previousRequest := catalog.Annotations[v1.MCPCatalogSyncAnnotation]
		req, response = catalogEntryRequest(t, storage, nil, http.MethodDelete, path, nil, values)
		require.NoError(t, handler.DeleteTunnelAssignment(req))
		require.Equal(t, http.StatusNoContent, response.Code)
		require.NoError(t, storage.Get(t.Context(), kclient.ObjectKeyFromObject(catalog), catalog))
		require.NotEmpty(t, catalog.Annotations[v1.MCPCatalogSyncAnnotation])
		require.NotEqual(t, previousRequest, catalog.Annotations[v1.MCPCatalogSyncAnnotation])
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

func TestVMCPUpgradeAdoptsCatalogTunnelAssignment(t *testing.T) {
	const (
		source = "https://example.com/catalog"
		target = "https://example.com/mcp"
	)
	entry := vmcpCatalogEntryForTest("entry")
	entry.Spec.Editable = false
	entry.Spec.SourceURL = source
	entry.Spec.Manifest.EntryKey = "inventory"
	entry.Spec.Manifest.RemoteConfig = &types.RemoteCatalogConfig{FixedURL: target}
	configuredTunnel := &v1.MCPTunnel{
		Name:      "mt1office",
		Namespace: system.DefaultNamespace,
		Spec: v1.MCPTunnelSpec{Manifest: types.MCPTunnelManifest{
			DisplayName: "Office",
			AllowedURLs: []string{target},
		}},
	}
	storage := newVMCPTestStorage(entry, configuredTunnel)
	handler := vmcpHandlerForTest(t, storage)
	admin := testUserWithRole("admin", types.GroupAdmin)
	created := callVMCPCreate(t, storage, newHandlerTestGateway(t), handler, testVMCPManifest(), admin)
	getComponent := func() types.VMCPComponent {
		t.Helper()
		var current v1.VMCP
		require.NoError(t, storage.Get(t.Context(), kclient.ObjectKey{Namespace: system.DefaultNamespace, Name: created.ID}, &current))
		require.Len(t, current.Spec.Manifest.Components, 1)
		return current.Spec.Manifest.Components[0]
	}
	upgrade := func() error {
		t.Helper()
		req, _ := catalogEntryRequest(t, storage, nil, http.MethodPost, "/api/vmcps/"+created.ID+"/trigger-update", nil, map[string]string{"vmcp_id": created.ID})
		return handler.TriggerUpdate(req)
	}

	original := getComponent()
	entry.Spec.TunnelAssignment = &v1.CatalogEntryTunnelAssignment{
		SourceURL:   source,
		EntryKey:    "inventory",
		TunnelName:  configuredTunnel.Name,
		ExpectedURL: target,
	}
	require.NoError(t, storage.Update(t.Context(), entry))
	require.Equal(t, original.SourceDigest, getComponent().SourceDigest)
	require.NoError(t, upgrade())
	adopted := getComponent()
	require.Equal(t, configuredTunnel.Name, adopted.CatalogEntry.Manifest.RemoteConfig.TunnelName)
	require.NotEqual(t, original.SourceDigest, adopted.SourceDigest)

	entry.Spec.Manifest.RemoteConfig.FixedURL += "/changed"
	require.NoError(t, storage.Update(t.Context(), entry))
	require.ErrorContains(t, upgrade(), "expectedURL must match")
	require.Equal(t, adopted.SourceDigest, getComponent().SourceDigest)

	entry.Spec.TunnelAssignment = nil
	require.NoError(t, storage.Update(t.Context(), entry))
	require.NoError(t, upgrade())
	require.Empty(t, getComponent().CatalogEntry.Manifest.RemoteConfig.TunnelName)
}

func TestCatalogPreviewsBlockUnassignedRequiredTunnel(t *testing.T) {
	catalog := &v1.MCPCatalog{
		Name:      "catalog",
		Namespace: system.DefaultNamespace,
	}
	entry := &v1.MCPServerCatalogEntry{
		Name:      "inventory",
		Namespace: system.DefaultNamespace,
		Spec: v1.MCPServerCatalogEntrySpec{
			MCPCatalogName: catalog.Name,
			SourceURL:      "https://example.com/catalog",
			Manifest: types.MCPServerCatalogEntryManifest{
				EntryKey: "inventory",
				Runtime:  types.RuntimeRemote,
				RemoteConfig: &types.RemoteCatalogConfig{
					FixedURL:       "http://127.0.0.1:8080/mcp",
					TunnelRequired: true,
				},
			},
		},
	}
	storage := newFakeStorage(t, catalog, entry)
	// No OAuth checker or session manager is installed. The handlers must
	// return before attempting discovery or opening a preview connection.
	handler := &MCPCatalogHandler{}
	values := map[string]string{"catalog_id": catalog.Name, "entry_id": entry.Name}
	path := "/api/mcp-catalogs/catalog/entries/inventory/generate-tool-previews"
	req, _ := catalogEntryRequest(t, storage, nil, http.MethodPost, path+"?dryRun=true", map[string]string{}, values)
	require.ErrorContains(t, handler.GenerateToolPreviews(req), "MCP tunnel assignment required")

	req, _ = catalogEntryRequest(t, storage, nil, http.MethodPost, path+"/oauth-url?dryRun=true", map[string]string{}, values)
	require.ErrorContains(t, handler.GenerateToolPreviewsOAuthURL(req), "MCP tunnel assignment required")
}
