package mcpcatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/obot-platform/nah/pkg/router"
	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/stretchr/testify/require"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCatalogSyncPreservesTunnelAssignmentAcrossRename(t *testing.T) {
	dir := t.TempDir()
	catalog := testCatalog()
	catalog.Spec.SourceURLs = []string{dir}
	client := &catalogAssignmentClearClient{WithWatch: newCatalogFakeClient(catalog)}
	handler := newParseTestHandler(t)
	ctx := t.Context()
	syncEntry := func(name, url string) v1.MCPServerCatalogEntry {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "entry.yaml"), []byte(
			"entryKey: inventory\nname: "+name+"\nshortDescription: Inventory\nruntime: remote\nremoteConfig:\n  fixedURL: "+url+"\n",
		), 0o600))
		require.NoError(t, client.Get(ctx, kclient.ObjectKeyFromObject(catalog), catalog))
		require.NoError(t, handler.SyncNow(ctx, client, router.Key(catalog.Namespace, catalog.Name)))
		require.NoError(t, client.Get(ctx, kclient.ObjectKeyFromObject(catalog), catalog))
		require.Empty(t, catalog.Status.SyncErrors)
		var entries v1.MCPServerCatalogEntryList
		require.NoError(t, client.List(ctx, &entries))
		require.Len(t, entries.Items, 1)
		return entries.Items[0]
	}

	const approvedURL = "https://1.1.1.1/mcp"
	entry := syncEntry("Inventory", approvedURL)
	assignment := v1.CatalogEntryTunnelAssignment{
		SourceURL:   dir,
		EntryKey:    "inventory",
		TunnelName:  "mt1office",
		ExpectedURL: approvedURL,
	}
	entry.Spec.TunnelAssignment = &assignment
	require.NoError(t, client.Update(ctx, &entry))

	const changedURL = "https://1.1.1.1/warehouse/mcp"
	renamed := syncEntry("Warehouse", changedURL)
	require.Equal(t, entry.Name, renamed.Name)
	require.Equal(t, "Warehouse", renamed.Spec.Manifest.Name)
	require.Equal(t, changedURL, renamed.Spec.Manifest.RemoteConfig.FixedURL)
	require.Empty(t, renamed.Spec.Manifest.RemoteConfig.TunnelName)
	require.Equal(t, &assignment, renamed.Spec.TunnelAssignment)

	client.clearBeforePatch = kclient.ObjectKeyFromObject(&renamed)
	renamed = syncEntry("Warehouse Inventory", changedURL)
	require.Nil(t, renamed.Spec.TunnelAssignment)
	require.Empty(t, client.clearBeforePatch.Name)
}

type catalogAssignmentClearClient struct {
	kclient.WithWatch
	clearBeforePatch kclient.ObjectKey
}

func (c *catalogAssignmentClearClient) Patch(ctx context.Context, obj kclient.Object, patch kclient.Patch, opts ...kclient.PatchOption) error {
	if c.clearBeforePatch.Name != "" && kclient.ObjectKeyFromObject(obj) == c.clearBeforePatch {
		// Model an administrator clearing the assignment after sync has
		// already decided which entry to patch.
		var current v1.MCPServerCatalogEntry
		if err := c.Get(ctx, c.clearBeforePatch, &current); err != nil {
			return err
		}
		current.Spec.TunnelAssignment = nil
		if err := c.Update(ctx, &current); err != nil {
			return err
		}
		c.clearBeforePatch = kclient.ObjectKey{}
	}
	return c.WithWatch.Patch(ctx, obj, patch, opts...)
}

func TestCatalogSyncAdoptsTunnelAssignment(t *testing.T) {
	dir := t.TempDir()
	catalog := testCatalog()
	catalog.Spec.SourceURLs = []string{dir}
	const target = "https://1.1.1.1/mcp"
	const changedTarget = "https://1.1.1.1/new/mcp"
	office := &v1.MCPTunnel{
		Name:      "mt1office",
		Namespace: catalog.Namespace,
		Spec: v1.MCPTunnelSpec{Manifest: types.MCPTunnelManifest{
			DisplayName: "Office",
			AllowedURLs: []string{"https://1.1.1.1/*"},
		}},
	}
	other := office.DeepCopy()
	other.Name = "mt1other"
	client := newCatalogFakeClient(catalog, office, other)
	handler := newParseTestHandler(t)
	ctx := t.Context()
	entryFile := filepath.Join(dir, "entry.yaml")
	vmcpFile := filepath.Join(dir, "vmcp.yaml")
	removedVMCPFile := filepath.Join(dir, "other-vmcp.yaml")
	writeEntry := func(url string) {
		t.Helper()
		require.NoError(t, os.WriteFile(entryFile, []byte(
			"entryKey: inventory\nname: Inventory\nshortDescription: Inventory\nruntime: remote\nremoteConfig:\n  fixedURL: "+url+"\n",
		), 0o600))
	}
	writeVMCP := func(path, key, name string) {
		t.Helper()
		require.NoError(t, os.WriteFile(path, []byte(
			"type: vmcp\nentryKey: "+key+"\ndisplayName: "+name+"\ncomponents:\n  - id: inventory\n    mcpServerCatalogEntryKey: inventory\n",
		), 0o600))
	}
	sync := func() {
		t.Helper()
		require.NoError(t, handler.SyncNow(ctx, client, router.Key(catalog.Namespace, catalog.Name)))
		require.NoError(t, client.Get(ctx, kclient.ObjectKeyFromObject(catalog), catalog))
	}
	entry := func() v1.MCPServerCatalogEntry {
		t.Helper()
		var list v1.MCPServerCatalogEntryList
		require.NoError(t, client.List(ctx, &list))
		require.Len(t, list.Items, 1)
		return list.Items[0]
	}
	component := func() types.VMCPComponent {
		t.Helper()
		var list v1.VMCPList
		require.NoError(t, client.List(ctx, &list))
		for _, vmcp := range list.Items {
			if vmcp.Spec.Manifest.DisplayName == "Bundle" {
				require.Len(t, vmcp.Spec.Manifest.Components, 1)
				return vmcp.Spec.Manifest.Components[0]
			}
		}
		t.Fatal("expected Bundle vMCP")
		return types.VMCPComponent{}
	}
	save := func(tunnel, expectedURL string) {
		t.Helper()
		current := entry()
		if tunnel == "" {
			current.Spec.TunnelAssignment = nil
		} else {
			current.Spec.TunnelAssignment = &v1.CatalogEntryTunnelAssignment{
				SourceURL:   dir,
				EntryKey:    "inventory",
				TunnelName:  tunnel,
				ExpectedURL: expectedURL,
			}
		}
		require.NoError(t, client.Update(ctx, &current))
	}

	writeEntry(target)
	writeVMCP(vmcpFile, "bundle", "Bundle")
	writeVMCP(removedVMCPFile, "other", "Other")
	sync()
	original := component()
	require.Empty(t, original.CatalogEntry.Manifest.RemoteConfig.TunnelName)

	save(office.Name, target)
	sync()
	assigned := component()
	require.Equal(t, office.Name, assigned.CatalogEntry.Manifest.RemoteConfig.TunnelName)
	require.NotEqual(t, original.SourceDigest, assigned.SourceDigest)
	require.Empty(t, entry().Spec.Manifest.RemoteConfig.TunnelName)

	writeEntry(changedTarget)
	require.NoError(t, os.Remove(removedVMCPFile))
	sync()
	require.Contains(t, catalog.Status.SyncErrors[dir], "expectedURL must match")
	require.Equal(t, assigned.SourceDigest, component().SourceDigest)
	require.Equal(t, changedTarget, entry().Spec.Manifest.RemoteConfig.FixedURL)
	var vmcps v1.VMCPList
	require.NoError(t, client.List(ctx, &vmcps))
	for _, vmcp := range vmcps.Items {
		if vmcp.Spec.Manifest.DisplayName == "Other" {
			require.False(t, vmcp.DeletionTimestamp.IsZero())
		}
	}

	save(other.Name, changedTarget)
	sync()
	require.Empty(t, catalog.Status.SyncErrors)
	require.Equal(t, other.Name, component().CatalogEntry.Manifest.RemoteConfig.TunnelName)

	save("", "")
	sync()
	require.Empty(t, component().CatalogEntry.Manifest.RemoteConfig.TunnelName)
}
