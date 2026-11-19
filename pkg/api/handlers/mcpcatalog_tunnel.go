package handlers

import (
	"fmt"
	"net/http"
	"slices"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/api"
	"github.com/obot-platform/obot/pkg/mcpcatalog"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type catalogTunnelAssignmentRequest struct {
	TunnelName  string `json:"tunnelName"`
	ExpectedURL string `json:"expectedURL"`
}

type catalogTunnelAssignmentResponse struct {
	catalogTunnelAssignmentRequest
	CurrentURL string `json:"currentURL"`
	Valid      bool   `json:"valid"`
	Reason     string `json:"reason,omitempty"`
}

func resolveTunnelAssignmentEntry(req api.Context) (*v1.MCPServerCatalogEntry, error) {
	// Auditors can read catalog inventory. Assignment endpoints require mutation privileges.
	if !req.UserIsAdmin() && !req.UserIsOwner() {
		return nil, types.NewErrForbidden("catalog tunnel assignments require an administrator")
	}

	var catalog v1.MCPCatalog
	if err := req.Get(&catalog, req.PathValue("catalog_id")); err != nil {
		return nil, err
	}
	return mcpcatalog.ResolveSourceEntry(req.Context(), req.Storage, &catalog, req.URL.Query().Get("sourceURL"), req.PathValue("entry_key"))
}

func tunnelAssignmentForRequest(req api.Context, entry *v1.MCPServerCatalogEntry) *v1.CatalogEntryTunnelAssignment {
	assignment := entry.Spec.TunnelAssignment
	if assignment == nil || assignment.SourceURL != req.URL.Query().Get("sourceURL") || assignment.EntryKey != req.PathValue("entry_key") {
		return nil
	}
	return assignment
}

func (*MCPCatalogHandler) GetTunnelAssignment(req api.Context) error {
	entry, err := resolveTunnelAssignmentEntry(req)
	if err != nil {
		return err
	}
	assignment := tunnelAssignmentForRequest(req, entry)
	if assignment == nil {
		return types.NewErrNotFound("tunnel assignment not found")
	}
	response := catalogTunnelAssignmentResponse{
		catalogTunnelAssignmentRequest: catalogTunnelAssignmentRequest{
			TunnelName:  assignment.TunnelName,
			ExpectedURL: assignment.ExpectedURL,
		},
		Valid: true,
	}
	if entry.Spec.Manifest.RemoteConfig != nil {
		response.CurrentURL = entry.Spec.Manifest.RemoteConfig.FixedURL
	}
	if _, err := mcpcatalog.ManifestWithTunnel(req.Context(), req.Storage, entry.Spec.Manifest, assignment.TunnelName, assignment.ExpectedURL); err != nil {
		response.Valid = false
		response.Reason = err.Error()
	}
	return req.Write(response)
}

func (*MCPCatalogHandler) PutTunnelAssignment(req api.Context) error {
	var input catalogTunnelAssignmentRequest
	if err := req.Read(&input); err != nil {
		return types.NewErrBadRequest("invalid tunnel assignment: %v", err)
	}
	if input.TunnelName == "" || input.ExpectedURL == "" {
		return types.NewErrBadRequest("tunnelName and expectedURL are required")
	}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		entry, err := resolveTunnelAssignmentEntry(req)
		if err != nil {
			return err
		}
		if _, err := mcpcatalog.ManifestWithTunnel(req.Context(), req.Storage, entry.Spec.Manifest, input.TunnelName, input.ExpectedURL); err != nil {
			return types.NewErrBadRequest("invalid tunnel assignment: %v", err)
		}
		assignment := v1.CatalogEntryTunnelAssignment{
			SourceURL:   req.URL.Query().Get("sourceURL"),
			EntryKey:    req.PathValue("entry_key"),
			TunnelName:  input.TunnelName,
			ExpectedURL: input.ExpectedURL,
		}
		if entry.Spec.TunnelAssignment != nil && *entry.Spec.TunnelAssignment == assignment {
			return nil
		}
		entry.Spec.TunnelAssignment = &assignment
		return req.Update(entry)
	}); err != nil {
		return err
	}
	return req.Write(catalogTunnelAssignmentResponse{
		catalogTunnelAssignmentRequest: input,
		CurrentURL:                     input.ExpectedURL,
		Valid:                          true,
	})
}

func (*MCPCatalogHandler) DeleteTunnelAssignment(req api.Context) error {
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		entry, err := resolveTunnelAssignmentEntry(req)
		if err != nil {
			if httpErr, ok := err.(*types.ErrHTTP); ok && httpErr.Code == http.StatusNotFound || apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		if tunnelAssignmentForRequest(req, entry) == nil {
			return nil
		}
		entry.Spec.TunnelAssignment = nil
		return req.Update(entry)
	}); err != nil {
		return err
	}
	req.WriteHeader(http.StatusNoContent)
	return nil
}

// A source can be re-added before sync deletes its old entries. Clear saved
// approvals before the URL becomes addressable again.
func clearReaddedSourceTunnelAssignments(req api.Context, catalog *v1.MCPCatalog, sources []string) error {
	added := make(map[string]struct{})
	for _, source := range sources {
		if !slices.Contains(catalog.Spec.SourceURLs, source) {
			added[source] = struct{}{}
		}
	}
	if len(added) == 0 {
		return nil
	}

	var entries v1.MCPServerCatalogEntryList
	if err := req.List(&entries, kclient.MatchingFields{"spec.mcpCatalogName": catalog.Name}); err != nil {
		return fmt.Errorf("failed to check tunnel assignments for added catalog sources: %w", err)
	}
	for _, entry := range entries.Items {
		if _, ok := added[entry.Spec.SourceURL]; !ok || entry.Spec.TunnelAssignment == nil {
			continue
		}
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			var current v1.MCPServerCatalogEntry
			if err := req.Get(&current, entry.Name); err != nil {
				if apierrors.IsNotFound(err) {
					return nil
				}
				return err
			}
			if _, ok := added[current.Spec.SourceURL]; !ok || current.Spec.MCPCatalogName != catalog.Name || current.Spec.TunnelAssignment == nil {
				return nil
			}
			current.Spec.TunnelAssignment = nil
			return req.Update(&current)
		}); err != nil {
			return fmt.Errorf("failed to clear tunnel assignment for re-added catalog source: %w", err)
		}
	}
	return nil
}
