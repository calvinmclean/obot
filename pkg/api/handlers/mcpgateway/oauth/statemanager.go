package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/obot-platform/obot/pkg/gateway/client"
	gatewaytypes "github.com/obot-platform/obot/pkg/gateway/types"
	"github.com/obot-platform/obot/pkg/mcp"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

// tokenClientFunc returns the HTTP client for the code exchange with a server-supplied
// token endpoint, going through the named tunnel when there is one.
type tokenClientFunc func(tunnelName string) (*http.Client, error)

type stateManager struct {
	gatewayClient *client.Client

	// tokenClient builds the client for the code exchange. When nil, or when it returns
	// no client, ExchangeOAuthToken falls back to its strict egress client.
	tokenClient   tokenClientFunc
	validateRoute func(context.Context, *gatewaytypes.MCPOAuthPendingState) error
}

func newStateManager(gatewayClient *client.Client, tokenClient tokenClientFunc) *stateManager {
	return &stateManager{
		gatewayClient: gatewayClient,
		tokenClient:   tokenClient,
	}
}

func (sm *stateManager) store(ctx context.Context, userID, initiatorUserID, mcpID, mcpURL, tunnelName, oauthAuthRequestID, state, verifier, resourceURL string, conf *oauth2.Config, upstreamChallenge ...string) error {
	return sm.storeForRoute(ctx, userID, initiatorUserID, mcpID, mcpURL, tunnelName, "", oauthAuthRequestID, state, verifier, resourceURL, conf, upstreamChallenge...)
}

func (sm *stateManager) storeForRoute(ctx context.Context, userID, initiatorUserID, mcpID, mcpURL, tunnelName, routeRevision, oauthAuthRequestID, state, verifier, resourceURL string, conf *oauth2.Config, upstreamChallenge ...string) error {
	return sm.gatewayClient.CreateMCPOAuthPendingStateForRoute(ctx, userID, initiatorUserID, mcpID, mcpURL, tunnelName, routeRevision, oauthAuthRequestID, state, verifier, resourceURL, conf, upstreamChallenge...)
}

func (sm *stateManager) createToken(ctx context.Context, state, code, errorStr, errorDescription string) (string, string, error) {
	ps, err := sm.gatewayClient.ConsumeMCPOAuthPendingState(ctx, state)
	if err != nil {
		return "", "", fmt.Errorf("failed to get oauth state: %w", err)
	}

	if errorStr != "" {
		return ps.OAuthAuthRequestID, ps.MCPID, fmt.Errorf("error returned from oauth server: %s, %s", errorStr, errorDescription)
	}

	if sm.validateRoute != nil {
		if err := sm.validateRoute(ctx, ps); err != nil {
			return ps.OAuthAuthRequestID, ps.MCPID, err
		}
	}
	var previousGrantID string
	if ps.RouteRevision != "" {
		current, err := sm.gatewayClient.GetMCPOAuthToken(ctx, ps.UserID, ps.MCPID, ps.URL)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return ps.OAuthAuthRequestID, ps.MCPID, err
		}
		if current != nil {
			previousGrantID = current.GrantID
		}
	}

	conf := &oauth2.Config{
		ClientID:     ps.ClientID,
		ClientSecret: ps.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:   ps.AuthURL,
			TokenURL:  ps.TokenURL,
			AuthStyle: ps.AuthStyle,
		},
		RedirectURL: ps.RedirectURL,
	}
	if ps.Scopes != "" {
		conf.Scopes = strings.Split(ps.Scopes, " ")
	}

	exchangeCtx := ctx
	if sm.tokenClient != nil {
		httpClient, err := sm.tokenClient(ps.TunnelName)
		if err != nil {
			return ps.OAuthAuthRequestID, ps.MCPID, fmt.Errorf("failed to create OAuth token client: %w", err)
		}

		if httpClient != nil {
			exchangeCtx = context.WithValue(ctx, oauth2.HTTPClient, httpClient)
		}
	}

	token, err := mcp.ExchangeOAuthToken(exchangeCtx, conf, code, ps.Verifier, ps.ResourceURL)
	if err != nil {
		return ps.OAuthAuthRequestID, ps.MCPID, fmt.Errorf("failed to exchange code: %w", err)
	}

	// The network exchange may outlive the route that authorized it.
	if sm.validateRoute != nil {
		if err := sm.validateRoute(ctx, ps); err != nil {
			return ps.OAuthAuthRequestID, ps.MCPID, err
		}
	}
	if ps.RouteRevision != "" {
		err = sm.gatewayClient.ReplaceMCPOAuthTokenForRoute(ctx, ps.UserID, ps.MCPID, ps.URL, ps.OAuthAuthRequestID, ps.RouteRevision, previousGrantID, conf, token, ps.UpstreamChallenge)
	} else {
		err = sm.gatewayClient.ReplaceMCPOAuthToken(ctx, ps.UserID, ps.MCPID, ps.URL, ps.OAuthAuthRequestID, conf, token, ps.UpstreamChallenge)
	}
	if err != nil {
		return ps.OAuthAuthRequestID, ps.MCPID, err
	}

	return ps.OAuthAuthRequestID, ps.MCPID, nil
}
