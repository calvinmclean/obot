package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/api"
	"github.com/obot-platform/obot/pkg/openapi"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
)

// ImportOpenAPI returns a snapshot for the catalog creation form without
// creating a draft entry. Uploaded files are sent as source.content (JSON/YAML).
func (h *MCPCatalogHandler) ImportOpenAPI(req api.Context) error {
	if catalogID := req.PathValue("catalog_id"); catalogID != "" {
		if err := req.Get(&v1.MCPCatalog{}, catalogID); err != nil {
			return err
		}
	} else if workspaceID := req.PathValue("workspace_id"); workspaceID != "" {
		if err := req.Get(&v1.PowerUserWorkspace{}, workspaceID); err != nil {
			return err
		}
	} else {
		return types.NewErrBadRequest("either catalog_id or workspace_id is required")
	}

	var config types.OpenAPIRuntimeConfig
	if err := req.Read(&config); err != nil {
		if syntaxErr, ok := errors.AsType[*json.SyntaxError](err); ok {
			return types.NewErrBadRequest("invalid OpenAPI import request JSON at byte %d: %v", syntaxErr.Offset, syntaxErr)
		}
		if typeErr, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
			return types.NewErrBadRequest("invalid OpenAPI import request field %q: expected %s at byte %d", typeErr.Field, typeErr.Type, typeErr.Offset)
		}
		if errors.Is(err, io.EOF) {
			return types.NewErrBadRequest("OpenAPI import request body is empty")
		}
		return err
	}
	// The importer uses its own bounded, SSRF-protected client. It never
	// receives request headers, user credentials, or a caller-provided client.
	result, err := h.openAPIImporter.Import(req.Context(), config)
	if err != nil {
		return types.NewErrBadRequest("failed to import OpenAPI schema: %v", err)
	}
	return req.Write(types.OpenAPIImportResponse{
		Schema:            result.Schema,
		BaseURL:           result.BaseURL,
		SuggestedHeaders:  append([]types.MCPConfig{}, result.SuggestedHeaders...),
		SuggestedMetadata: result.SuggestedMetadata,
	})
}

// prepareOpenAPIEntry accepts either an imported snapshot or a source to import.
// Updating settings alone keeps the previous snapshot; importing a new snapshot
// is an explicit action. Nothing here updates deployed server manifests.
func (h *MCPCatalogHandler) prepareOpenAPIEntry(ctx context.Context, manifest *types.MCPServerCatalogEntryManifest, previous *types.OpenAPIRuntimeConfig) error {
	if manifest.Runtime != types.RuntimeOpenAPI || manifest.OpenAPIConfig == nil {
		return nil // The runtime validator reports missing configuration.
	}
	config := manifest.OpenAPIConfig.DeepCopy()
	if err := openapi.ValidateSource(config.Source); err != nil {
		return err
	}
	if len(config.Schema) == 0 && previous != nil && config.Source == previous.Source {
		config.Schema = slices.Clone(previous.Schema)
	}

	var result *openapi.Result
	var err error
	if len(config.Schema) == 0 {
		result, err = h.openAPIImporter.Import(ctx, *config)
	} else {
		// Treat submitted snapshots as untrusted input: validate and normalize
		// them, but do not refetch a URL that may have changed since preview.
		result, err = openapi.Parse(config.Schema, *config)
	}
	if err != nil {
		return err
	}
	config.Schema = result.Schema
	manifest.OpenAPIConfig = config
	return nil
}
