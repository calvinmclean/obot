package mcpcatalog

import (
	"context"
	"fmt"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/mcp"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/tunnel"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// ManifestWithTunnel validates the approved URL and projects the tunnel ID
// onto a copy so the Git definition stays independent of the assignment.
func ManifestWithTunnel(ctx context.Context, client kclient.Client, manifest types.MCPServerCatalogEntryManifest, tunnelName, expectedURL string) (types.MCPServerCatalogEntryManifest, error) {
	if manifest.Runtime != types.RuntimeRemote || manifest.RemoteConfig == nil || manifest.RemoteConfig.FixedURL == "" {
		return manifest, fmt.Errorf("tunnel assignments require a synced remote entry with a literal fixed URL")
	}
	if expectedURL != manifest.RemoteConfig.FixedURL {
		return manifest, fmt.Errorf("expectedURL must match the current Git catalog entry fixedURL")
	}
	effective := manifest
	remote := *manifest.RemoteConfig
	remote.TunnelName = tunnelName
	effective.RemoteConfig = &remote
	if err := (mcp.RemoteValidator{}).ValidateCatalogConfig(ctx, effective); err != nil {
		return manifest, err
	}
	if err := tunnel.ValidateReference(ctx, client, tunnelName, expectedURL); err != nil {
		return manifest, err
	}
	return effective, nil
}

// EffectiveManifest applies only the approval for this Git entry. During
// sync, desired contains the incoming definition and saved holds admin state.
func EffectiveManifest(ctx context.Context, client kclient.Client, desired, saved *v1.MCPServerCatalogEntry) (types.MCPServerCatalogEntryManifest, error) {
	manifest := desired.Spec.Manifest
	assignment := saved.Spec.TunnelAssignment
	if assignment == nil || !desired.IsGitManaged() || !saved.IsGitManaged() ||
		desired.Spec.MCPCatalogName != saved.Spec.MCPCatalogName ||
		assignment.SourceURL != desired.Spec.SourceURL || assignment.EntryKey != manifest.EntryKey {
		return manifest, nil
	}
	return ManifestWithTunnel(ctx, client, manifest, assignment.TunnelName, assignment.ExpectedURL)
}
