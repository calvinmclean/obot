package mcpcatalog

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/obot-platform/nah/pkg/router"
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
