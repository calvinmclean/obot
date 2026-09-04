package mcp

import (
	"fmt"

	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
)

// MCPServerCredentialContext returns the credential context for an MCP server.
func MCPServerCredentialContext(server v1.MCPServer) string {
	if credentialContext := CatalogOrPowerUserWorkspaceServerCredentialContext(server); credentialContext != "" {
		return credentialContext
	}
	return MCPServerCredentialContextForUser(server.Spec.UserID, server.Name)
}

// CatalogOrPowerUserWorkspaceServerCredentialContext returns the shared credential
// context for a catalog or power-user workspace server. It returns an empty string
// for a user-owned server, whose credentials are scoped separately for each user.
func CatalogOrPowerUserWorkspaceServerCredentialContext(server v1.MCPServer) string {
	if server.Spec.IsCatalogServer() {
		return MCPServerCredentialContextForScope(server.Spec.MCPCatalogID, server.Name)
	}
	if server.Spec.IsPowerUserWorkspaceServer() {
		return MCPServerCredentialContextForScope(server.Spec.PowerUserWorkspaceID, server.Name)
	}
	return ""
}

// MCPServerCredentialContextForUser returns the credential context for a user's MCP server.
func MCPServerCredentialContextForUser(userID, serverName string) string {
	return MCPServerCredentialContextForScope(userID, serverName)
}

// MCPServerCredentialContextForScope returns the credential context for an MCP server
// under an explicitly selected catalog, workspace, or user scope.
func MCPServerCredentialContextForScope(scope, serverName string) string {
	return fmt.Sprintf("%s-%s", scope, serverName)
}
