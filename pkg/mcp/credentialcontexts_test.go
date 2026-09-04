package mcp

import (
	"testing"

	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/stretchr/testify/require"
)

func TestMCPServerCredentialContext(t *testing.T) {
	for _, test := range []struct {
		name   string
		server v1.MCPServer
		want   string
	}{
		{
			name:   "user",
			server: v1.MCPServer{Name: "server", Spec: v1.MCPServerSpec{UserID: "user"}},
			want:   "user-server",
		},
		{
			name:   "catalog",
			server: v1.MCPServer{Name: "server", Spec: v1.MCPServerSpec{UserID: "user", MCPCatalogID: "catalog"}},
			want:   "catalog-server",
		},
		{
			name:   "power user workspace",
			server: v1.MCPServer{Name: "server", Spec: v1.MCPServerSpec{UserID: "user", PowerUserWorkspaceID: "workspace"}},
			want:   "workspace-server",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, MCPServerCredentialContext(test.server))
		})
	}
}

func TestCatalogOrPowerUserWorkspaceServerCredentialContext(t *testing.T) {
	require.Empty(t, CatalogOrPowerUserWorkspaceServerCredentialContext(v1.MCPServer{
		Name: "server",
		Spec: v1.MCPServerSpec{UserID: "user"},
	}))
}

func TestExplicitMCPServerCredentialContexts(t *testing.T) {
	require.Equal(t, "user-server", MCPServerCredentialContextForUser("user", "server"))
	require.Equal(t, "scope-server", MCPServerCredentialContextForScope("scope", "server"))
}
