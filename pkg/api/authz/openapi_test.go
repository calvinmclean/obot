package authz

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	storagescheme "github.com/obot-platform/obot/pkg/storage/scheme"
	"github.com/obot-platform/obot/pkg/system"
	"github.com/stretchr/testify/require"
	"k8s.io/apiserver/pkg/authentication/user"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestOpenAPIImportAuthorization(t *testing.T) {
	storage := clientfake.NewClientBuilder().WithScheme(storagescheme.Scheme).WithObjects(&v1.PowerUserWorkspace{
		Name:      "workspace",
		Namespace: system.DefaultNamespace,
		Spec: v1.PowerUserWorkspaceSpec{
			UserID: "workspace-owner",
		},
	}).Build()
	authorizer := NewAuthorizer(nil, storage, storage, false, nil, nil, nil, false)
	for _, test := range []struct {
		name      string
		role      types.Role
		userID    string
		catalog   bool
		workspace bool
	}{
		{
			name:      "admin",
			role:      types.RoleAdmin,
			userID:    "admin",
			catalog:   true,
			workspace: true,
		},
		{
			name:      "workspace owner",
			role:      types.RolePowerUser,
			userID:    "workspace-owner",
			workspace: true,
		},
		{
			name:   "other power user",
			role:   types.RolePowerUser,
			userID: "other",
		},
		{
			name:   "basic user",
			role:   types.RoleBasic,
			userID: "workspace-owner",
		},
		{
			name:   "auditor",
			role:   types.RoleAuditor,
			userID: "auditor",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			principal := &user.DefaultInfo{Name: test.userID, UID: test.userID, Groups: test.role.Groups()}
			require.Equal(t, test.catalog, authorizer.Authorize(
				httptest.NewRequest(http.MethodPost, "/api/mcp-catalogs/default/openapi/import", nil), principal))
			require.Equal(t, test.workspace, authorizer.Authorize(
				httptest.NewRequest(http.MethodPost, "/api/workspaces/workspace/openapi/import", nil), principal))
		})
	}
	anonymous := &user.DefaultInfo{Name: "anonymous", Groups: []string{UnauthenticatedGroup}}
	require.False(t, authorizer.Authorize(httptest.NewRequest(http.MethodPost, "/api/mcp-catalogs/default/openapi/import", nil), anonymous))
	require.False(t, authorizer.Authorize(httptest.NewRequest(http.MethodPost, "/api/workspaces/workspace/openapi/import", nil), anonymous))
}
