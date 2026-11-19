package handlers

import (
	"fmt"
	"strings"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/api"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/system"
	tunnelpkg "github.com/obot-platform/obot/pkg/tunnel"
)

type mcpTunnelConnectionCloser interface {
	DisconnectCredential(name, credentialID string)
}

type MCPTunnelHandler struct {
	tunnels mcpTunnelConnectionCloser
}

func NewMCPTunnelHandler(tunnels mcpTunnelConnectionCloser) *MCPTunnelHandler {
	return &MCPTunnelHandler{tunnels: tunnels}
}

func (*MCPTunnelHandler) List(req api.Context) error {
	var list v1.MCPTunnelList
	if err := req.List(&list); err != nil {
		return fmt.Errorf("failed to list MCP tunnels: %w", err)
	}

	items := make([]types.MCPTunnel, 0, len(list.Items))
	for _, item := range list.Items {
		converted, err := convertMCPTunnel(item, "")
		if err != nil {
			return fmt.Errorf("failed to convert MCP tunnel %q: %w", item.Name, err)
		}
		items = append(items, converted)
	}

	return req.Write(types.MCPTunnelList{Items: items})
}

func (*MCPTunnelHandler) Get(req api.Context) error {
	var tunnel v1.MCPTunnel
	if err := req.Get(&tunnel, req.PathValue("id")); err != nil {
		return fmt.Errorf("failed to get MCP tunnel: %w", err)
	}

	converted, err := convertMCPTunnel(tunnel, "")
	if err != nil {
		return fmt.Errorf("failed to convert MCP tunnel %q: %w", tunnel.Name, err)
	}
	return req.Write(converted)
}

func (*MCPTunnelHandler) Create(req api.Context) error {
	manifest, err := readAndValidateMCPTunnelManifest(req)
	if err != nil {
		return err
	}

	token, credential, err := tunnelpkg.NewCredential()
	if err != nil {
		return err
	}
	credentialID, err := tunnelpkg.CredentialID(credential)
	if err != nil {
		return err
	}

	tunnel := v1.MCPTunnel{
		GenerateName: system.MCPTunnelPrefix,
		Namespace:    req.Namespace(),
		Spec: v1.MCPTunnelSpec{
			Manifest:     manifest,
			Credential:   credential,
			CredentialID: credentialID,
		},
	}
	if err := req.Create(&tunnel); err != nil {
		return fmt.Errorf("failed to create MCP tunnel: %w", err)
	}

	converted, err := convertMCPTunnel(tunnel, token)
	if err != nil {
		return fmt.Errorf("failed to convert MCP tunnel %q: %w", tunnel.Name, err)
	}
	return req.WriteCreated(converted)
}

func (*MCPTunnelHandler) Update(req api.Context) error {
	manifest, err := readAndValidateMCPTunnelManifest(req)
	if err != nil {
		return err
	}

	var stored v1.MCPTunnel
	if err := withTunnelPolicyLock(req, func() error {
		if err := req.Get(&stored, req.PathValue("id")); err != nil {
			return fmt.Errorf("failed to get MCP tunnel: %w", err)
		}
		dependencies, err := tunnelpkg.ListDependencies(req.Context(), req.Storage, req.Namespace(), stored.Name)
		if err != nil {
			return fmt.Errorf("failed to list MCP tunnel dependencies: %w", err)
		}
		var blocked []string
		for _, dependency := range dependencies {
			if !manifest.AllowsURL(dependency.Target) {
				blocked = append(blocked, fmt.Sprintf("%s (target %q)", dependency.Description, dependency.Target))
			}
		}
		if len(blocked) > 0 {
			return types.NewErrBadRequest("MCP tunnel %q cannot be updated because its allowlist would exclude: %s",
				mcpTunnelDisplayName(stored), strings.Join(blocked, ", "))
		}
		stored.Spec.Manifest = manifest
		if err := req.Update(&stored); err != nil {
			return fmt.Errorf("failed to update MCP tunnel: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	converted, err := convertMCPTunnel(stored, "")
	if err != nil {
		return fmt.Errorf("failed to convert MCP tunnel %q: %w", stored.Name, err)
	}
	return req.Write(converted)
}

func (h *MCPTunnelHandler) Delete(req api.Context) error {
	var stored v1.MCPTunnel
	if err := withTunnelPolicyLock(req, func() error {
		if err := req.Get(&stored, req.PathValue("id")); err != nil {
			return fmt.Errorf("failed to get MCP tunnel: %w", err)
		}
		dependencies, err := tunnelpkg.ListDependencies(req.Context(), req.Storage, req.Namespace(), stored.Name)
		if err != nil {
			return fmt.Errorf("failed to list MCP tunnel dependencies: %w", err)
		}
		if len(dependencies) > 0 {
			blocked := make([]string, 0, len(dependencies))
			for _, dependency := range dependencies {
				blocked = append(blocked, dependency.Description)
			}
			return types.NewErrBadRequest("MCP tunnel %q cannot be deleted because it is used by: %s",
				mcpTunnelDisplayName(stored), strings.Join(blocked, ", "))
		}
		if err := req.Delete(&stored); err != nil {
			return fmt.Errorf("failed to delete MCP tunnel: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	credentialID := stored.Spec.CredentialID
	if credentialID == "" {
		credentialID, _ = tunnelpkg.CredentialID(stored.Spec.Credential)
	}
	if h.tunnels != nil {
		h.tunnels.DisconnectCredential(stored.Name, credentialID)
	}
	return req.Write(map[string]string{"deleted": stored.Name})
}

func withTunnelPolicyLock(req api.Context, fn func() error) error {
	if req.GatewayClient == nil {
		return fn()
	}
	return req.GatewayClient.WithTunnelPolicyLock(req.Context(), fn)
}

func mcpTunnelDisplayName(tunnel v1.MCPTunnel) string {
	if displayName := strings.TrimSpace(tunnel.Spec.Manifest.DisplayName); displayName != "" {
		return displayName
	}
	return tunnel.Name
}

func (h *MCPTunnelHandler) RotateSecret(req api.Context) error {
	var tunnel v1.MCPTunnel
	if err := req.Get(&tunnel, req.PathValue("id")); err != nil {
		return fmt.Errorf("failed to get MCP tunnel: %w", err)
	}

	previousCredentialID := tunnel.Spec.CredentialID
	if previousCredentialID == "" {
		// If this produces an error, then it's not possible to use the previous credential ID,
		// so we don't need to worry about disconnecting it.
		previousCredentialID, _ = tunnelpkg.CredentialID(tunnel.Spec.Credential)
	}
	token, credential, err := tunnelpkg.NewCredential()
	if err != nil {
		return err
	}
	credentialID, err := tunnelpkg.CredentialID(credential)
	if err != nil {
		return err
	}

	tunnel.Spec.Credential = credential
	tunnel.Spec.CredentialID = credentialID
	if err := req.Update(&tunnel); err != nil {
		return fmt.Errorf("failed to rotate MCP tunnel secret: %w", err)
	}
	if h.tunnels != nil {
		h.tunnels.DisconnectCredential(tunnel.Name, previousCredentialID)
	}

	converted, err := convertMCPTunnel(tunnel, token)
	if err != nil {
		return fmt.Errorf("failed to convert MCP tunnel %q: %w", tunnel.Name, err)
	}
	return req.Write(converted)
}

func readAndValidateMCPTunnelManifest(req api.Context) (types.MCPTunnelManifest, error) {
	var manifest types.MCPTunnelManifest
	if err := req.Read(&manifest); err != nil {
		return types.MCPTunnelManifest{}, types.NewErrBadRequest("failed to read MCP tunnel manifest: %v", err)
	}

	manifest.DisplayName = strings.TrimSpace(manifest.DisplayName)
	manifest.Description = strings.TrimSpace(manifest.Description)
	for i := range manifest.AllowedURLs {
		manifest.AllowedURLs[i] = strings.TrimSpace(manifest.AllowedURLs[i])
	}
	if err := manifest.Validate(); err != nil {
		return types.MCPTunnelManifest{}, types.NewErrBadRequest("invalid MCP tunnel: %v", err)
	}

	return manifest, nil
}

func convertMCPTunnel(tunnel v1.MCPTunnel, token string) (types.MCPTunnel, error) {
	if token == "" {
		var err error
		token, err = tunnelpkg.CredentialPreview(tunnel.Spec.Credential)
		if err != nil {
			return types.MCPTunnel{}, err
		}
	}

	return types.MCPTunnel{
		Metadata: MetadataFrom(&tunnel),
		Manifest: tunnel.Spec.Manifest,
		Token:    token,
	}, nil
}
