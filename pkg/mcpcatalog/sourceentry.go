package mcpcatalog

import (
	"context"
	"net/url"
	"slices"

	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// ResolveSourceEntry resolves the current entry object after a rename. Entry
// keys are scoped to the exact configured source, including its ref and path.
func ResolveSourceEntry(ctx context.Context, client kclient.Client, catalog *v1.MCPCatalog, sourceURL, entryKey string) (*v1.MCPServerCatalogEntry, error) {
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed.User != nil {
		return nil, types.NewErrBadRequest("sourceURL must be a valid source URL without embedded credentials")
	}
	if sourceURL == "" || entryKey == "" || !slices.Contains(catalog.Spec.SourceURLs, sourceURL) {
		return nil, types.NewErrNotFound("synced catalog entry not found")
	}

	var entries v1.MCPServerCatalogEntryList
	if err := client.List(ctx, &entries, kclient.InNamespace(catalog.Namespace), kclient.MatchingFields{"spec.mcpCatalogName": catalog.Name}); err != nil {
		return nil, err
	}
	var match *v1.MCPServerCatalogEntry
	for i := range entries.Items {
		entry := &entries.Items[i]
		if entry.IsGitManaged() && entry.Spec.SourceURL == sourceURL && entry.Spec.Manifest.EntryKey == entryKey {
			if match != nil {
				return nil, types.NewErrAlreadyExists("synced catalog entry key is ambiguous")
			}
			match = entry
		}
	}
	if match == nil {
		return nil, types.NewErrNotFound("synced catalog entry not found")
	}
	return match, nil
}
