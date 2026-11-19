package vmcp

import (
	"testing"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/stretchr/testify/require"
)

func TestNeedsUpdate(t *testing.T) {
	source := types.MCPServerCatalogEntrySnapshot{Manifest: types.MCPServerCatalogEntryManifest{
		Name:        "server",
		Description: "original",
		Runtime:     types.RuntimeRemote,
		Config:      []types.MCPConfig{{Key: "TOKEN", Value: "fixed"}},
	}}
	informational := source
	informational.Manifest.Metadata = map[string]string{"categories": "Developer Tools"}
	informational.Manifest.ShortDescription = "new short description"
	informational.Manifest.Description = "new description"
	informational.Manifest.Icon = "https://example.com/icon.png"
	informational.Manifest.UpgradeNote = "Read before upgrading."
	informational.Manifest.RepoURL = "https://github.com/example/server"
	informational.Manifest.EntryKey = "new-entry-key"
	informational.Manifest.ToolPreview = []types.MCPServerTool{{Name: "tool"}}
	informational.Manifest.MinObotVersion = "v0.27.0"
	informational.Manifest.MaxObotVersion = "v0.30.0"
	changed := source
	changed.Manifest.Config = []types.MCPConfig{{Key: "TOKEN", Value: "changed"}}

	component := types.VMCPComponent{CatalogEntry: source, SourceDigest: SourceDigest(source)}
	if NeedsUpdate(component, source) {
		t.Fatal("unchanged source reported an update")
	}
	if NeedsUpdate(component, informational) {
		t.Fatal("informational fields reported an update")
	}
	if !NeedsUpdate(component, changed) {
		t.Fatal("configuration change did not report an update")
	}

	// The source digest migration keeps legacy digests of stripped snapshots
	// whose source had drifted. They match again if the source reverts.
	stripped := source
	stripped.Manifest.Config = []types.MCPConfig{{Key: "TOKEN"}}
	for _, digest := range LegacySourceDigests(source) {
		legacy := types.VMCPComponent{CatalogEntry: stripped, SourceDigest: digest}
		if !NeedsUpdate(legacy, changed) {
			t.Fatal("configuration change did not report an update for a legacy digest")
		}
		if NeedsUpdate(legacy, source) {
			t.Fatal("reverted source reported an update for a legacy digest")
		}
	}
}

func TestVersionOAuthRouteRequiresFreshAuthorizationOnReturn(t *testing.T) {
	direct := types.MCPServerCatalogEntrySnapshot{
		Manifest: types.MCPServerCatalogEntryManifest{
			Runtime:      types.RuntimeRemote,
			RemoteConfig: &types.RemoteCatalogConfig{FixedURL: "https://provider.example/mcp"},
		},
	}
	tunneled := *direct.DeepCopy()
	tunneled.Manifest.RemoteConfig.TunnelName = "mt1office"
	VersionOAuthRoute(&tunneled, direct)
	require.NotEmpty(t, tunneled.OAuthRouteRevision)
	unchanged := *tunneled.DeepCopy()
	VersionOAuthRoute(&unchanged, tunneled)
	require.Equal(t, tunneled.OAuthRouteRevision, unchanged.OAuthRouteRevision)
	VersionOAuthRoute(&direct, tunneled)
	require.NotEmpty(t, direct.OAuthRouteRevision)
	require.NotEqual(t, tunneled.OAuthRouteRevision, direct.OAuthRouteRevision)
	returned := *tunneled.DeepCopy()
	VersionOAuthRoute(&returned, direct)
	require.NotEqual(t, tunneled.OAuthRouteRevision, returned.OAuthRouteRevision)
	require.Equal(t, SourceDigest(tunneled), SourceDigest(returned))
}
