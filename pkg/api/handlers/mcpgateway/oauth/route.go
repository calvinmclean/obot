package oauth

import (
	"context"

	"github.com/obot-platform/obot/apiclient/types"
	gatewaytypes "github.com/obot-platform/obot/pkg/gateway/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/system"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// validatePendingRoute checks the deployed backing server. Preview IDs are
// derived from their temporary configuration and have no backing object.
func (f *MCPOAuthHandlerFactory) validatePendingRoute(ctx context.Context, pending *gatewaytypes.MCPOAuthPendingState) error {
	changed := types.NewErrBadRequest("upstream OAuth route changed; start a new authorization")
	if f.client == nil {
		if pending.RouteRevision != "" {
			return changed
		}
		return nil
	}
	serverName := pending.MCPID
	switch {
	case system.IsMCPServerInstanceID(pending.MCPID):
		var instance v1.MCPServerInstance
		if err := f.client.Get(ctx, kclient.ObjectKey{Namespace: system.DefaultNamespace, Name: pending.MCPID}, &instance); err != nil {
			if apierrors.IsNotFound(err) && pending.RouteRevision == "" {
				return nil
			}
			return err
		}
		serverName = instance.Spec.MCPServerName
	case !system.IsMCPServerID(pending.MCPID):
		if pending.RouteRevision != "" {
			return changed
		}
		return nil
	}

	var server v1.MCPServer
	if err := f.client.Get(ctx, kclient.ObjectKey{Namespace: system.DefaultNamespace, Name: serverName}, &server); err != nil {
		if apierrors.IsNotFound(err) && pending.RouteRevision == "" {
			return nil
		}
		return err
	}
	if pending.RouteRevision != server.Spec.OAuthRouteRevision {
		return changed
	}
	if pending.RouteRevision != "" {
		remote := server.Spec.Manifest.RemoteConfig
		if remote == nil || remote.URL != pending.URL || remote.TunnelName != pending.TunnelName {
			return changed
		}
	}
	return nil
}
