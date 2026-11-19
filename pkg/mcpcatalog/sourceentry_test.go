package mcpcatalog

import (
	"testing"

	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolveSourceEntry(t *testing.T) {
	const source = "https://example.com/catalog?ref=main"
	catalog := &v1.MCPCatalog{
		ObjectMeta: metav1.ObjectMeta{Name: "catalog", Namespace: "default"},
		Spec: v1.MCPCatalogSpec{
			SourceURLs: []string{source},
		},
	}
	entry := &v1.MCPServerCatalogEntry{
		ObjectMeta: metav1.ObjectMeta{Name: "renamed-entry", Namespace: "default"},
		Spec: v1.MCPServerCatalogEntrySpec{
			MCPCatalogName: catalog.Name,
			SourceURL:      source,
			Manifest:       types.MCPServerCatalogEntryManifest{EntryKey: "inventory"},
		},
	}
	otherSource := entry.DeepCopy()
	otherSource.Name = "other-source-entry"
	otherSource.Spec.SourceURL = "https://example.com/catalog?ref=dev"
	detached := entry.DeepCopy()
	detached.Name = "detached-entry"
	detached.Spec.Detached = true

	scheme := runtime.NewScheme()
	scheme.AddKnownTypes(v1.SchemeGroupVersion, &v1.MCPServerCatalogEntry{}, &v1.MCPServerCatalogEntryList{})
	client := fake.NewClientBuilder().WithScheme(scheme).
		WithIndex(&v1.MCPServerCatalogEntry{}, "spec.mcpCatalogName", func(obj kclient.Object) []string {
			return []string{obj.(*v1.MCPServerCatalogEntry).Spec.MCPCatalogName}
		}).
		WithObjects(entry, otherSource, detached).Build()

	got, err := ResolveSourceEntry(t.Context(), client, catalog, source, "inventory")
	require.NoError(t, err)
	require.Equal(t, entry.Name, got.Name)

	_, err = ResolveSourceEntry(t.Context(), client, catalog, otherSource.Spec.SourceURL, "inventory")
	require.ErrorContains(t, err, "synced catalog entry not found")

	_, err = ResolveSourceEntry(t.Context(), client, catalog, "https://token@example.com/catalog?ref=main", "inventory")
	require.ErrorContains(t, err, "without embedded credentials")

	duplicate := entry.DeepCopy()
	duplicate.Name = "duplicate-entry"
	duplicate.ResourceVersion = ""
	require.NoError(t, client.Create(t.Context(), duplicate))
	_, err = ResolveSourceEntry(t.Context(), client, catalog, source, "inventory")
	require.ErrorContains(t, err, "synced catalog entry key is ambiguous")
}
