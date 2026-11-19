package tunnel

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/vmcp"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type routeReference struct {
	tunnel string
	target string
}

// Dependency identifies a saved or deployed configuration that uses a tunnel.
type Dependency struct {
	Description string
	Target      string
}

// ListDependencies reads both definitions and deployed routes. Backing
// servers can still serve the previous route while a snapshot rolls out.
func ListDependencies(ctx context.Context, client kclient.Client, namespace, tunnelName string) ([]Dependency, error) {
	var (
		entries     v1.MCPServerCatalogEntryList
		definitions v1.VMCPList
		instances   v1.VMCPInstanceList
		servers     v1.MCPServerList
	)
	for _, list := range []kclient.ObjectList{&entries, &definitions, &instances, &servers} {
		if err := client.List(ctx, list, kclient.InNamespace(namespace)); err != nil {
			return nil, err
		}
	}

	var dependencies []Dependency
	add := func(ref routeReference, description string) {
		if ref.tunnel == tunnelName {
			dependencies = append(dependencies, Dependency{Description: description, Target: ref.target})
		}
	}
	for _, entry := range entries.Items {
		description := fmt.Sprintf("catalog entry %s", entry.Name)
		if displayName := strings.TrimSpace(entry.Spec.Manifest.Name); displayName != "" && displayName != entry.Name {
			description = fmt.Sprintf("catalog entry %q (%s)", displayName, entry.Name)
		}
		add(catalogRoute(entry.Spec.Manifest), description)
		add(assignmentRoute(entry), description+" saved assignment")
	}

	byName := make(map[string]*v1.VMCP, len(definitions.Items))
	for i := range definitions.Items {
		definition := &definitions.Items[i]
		byName[definition.Name] = definition
		for _, component := range definition.Spec.Manifest.Components {
			add(catalogRoute(component.CatalogEntry.Manifest), fmt.Sprintf("vMCP %s component %s", definition.Name, component.ID))
		}
	}
	for _, instance := range instances.Items {
		if definition := byName[instance.Spec.Manifest.VMCPID]; definition != nil {
			for _, component := range retainedComponents(definition, &instance) {
				add(catalogRoute(component.CatalogEntry.Manifest), fmt.Sprintf("vMCP connection %s component %s", instance.Name, component.ID))
			}
		}
	}
	for _, server := range servers.Items {
		add(runtimeRoute(server.Spec.Manifest), fmt.Sprintf("MCP server %s", server.Name))
	}

	slices.SortFunc(dependencies, func(a, b Dependency) int {
		if diff := strings.Compare(a.Description, b.Description); diff != 0 {
			return diff
		}
		return strings.Compare(a.Target, b.Target)
	})
	return slices.Compact(dependencies), nil
}

func catalogRoute(manifest types.MCPServerCatalogEntryManifest) routeReference {
	remote := manifest.RemoteConfig
	if manifest.Runtime != types.RuntimeRemote || remote == nil {
		return routeReference{}
	}
	target := remote.FixedURL
	if target == "" {
		target = remote.Hostname
	}
	return routeReference{tunnel: remote.TunnelName, target: target}
}

func runtimeRoute(manifest types.MCPServerManifest) routeReference {
	remote := manifest.RemoteConfig
	if manifest.Runtime != types.RuntimeRemote || remote == nil {
		return routeReference{}
	}
	target := remote.URL
	if target == "" {
		target = remote.Hostname
	}
	return routeReference{tunnel: remote.TunnelName, target: target}
}

func assignmentRoute(entry v1.MCPServerCatalogEntry) routeReference {
	assignment := entry.Spec.TunnelAssignment
	if assignment == nil || !entry.IsGitManaged() ||
		assignment.SourceURL != entry.Spec.SourceURL || assignment.EntryKey != entry.Spec.Manifest.EntryKey {
		return routeReference{}
	}
	return routeReference{tunnel: assignment.TunnelName, target: assignment.ExpectedURL}
}

func retainedComponents(definition *v1.VMCP, instance *v1.VMCPInstance) []types.VMCPComponent {
	if len(instance.Spec.LegacyComponents) == 0 {
		return nil
	}
	var retained []types.VMCPComponent
	for _, component := range definition.Spec.Manifest.Components {
		if slices.Contains(instance.Spec.LegacyDisabledComponents, component.ID) {
			continue
		}
		if legacy, ok := vmcp.LegacyComponent(*definition, *instance, component); ok {
			retained = append(retained, legacy)
		}
	}
	return retained
}
