package apiclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/obot-platform/obot/apiclient/types"
)

type ListMCPServersOptions struct {
	CatalogID   string
	WorkspaceID string
}

func (c *Client) GetMCPServer(ctx context.Context, id string, opts ListMCPServersOptions) (*types.MCPServer, error) {
	url := fmt.Sprintf("/mcp-servers/%s", id)
	if opts.CatalogID != "" {
		url = fmt.Sprintf("/mcp-catalogs/%s/servers/%s", opts.CatalogID, id)
	} else if opts.WorkspaceID != "" {
		url = fmt.Sprintf("/workspaces/%s/servers/%s", opts.WorkspaceID, id)
	}

	_, resp, err := c.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return toObject(resp, &types.MCPServer{})
}

func (c *Client) ListMCPServers(ctx context.Context, opts ListMCPServersOptions) (result types.MCPServerList, err error) {
	url := "/mcp-servers"
	if opts.CatalogID != "" {
		url = fmt.Sprintf("/mcp-catalogs/%s/servers", opts.CatalogID)
	} else if opts.WorkspaceID != "" {
		url = fmt.Sprintf("/workspaces/%s/servers", opts.WorkspaceID)
	}

	_, resp, err := c.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	_, err = toObject(resp, &result)
	return result, err
}

func (c *Client) GetProjectMCPServer(ctx context.Context, assistantID, projectID, id string) (*types.ProjectMCPServer, error) {
	url := fmt.Sprintf("/assistants/%s/projects/%s/mcpservers/%s", assistantID, projectID, id)
	_, resp, err := c.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return toObject(resp, &types.ProjectMCPServer{})
}

func (c *Client) ListProjectMCPServers(ctx context.Context, assistantID, projectID string) (result types.ProjectMCPServerList, err error) {
	url := fmt.Sprintf("/assistants/%s/projects/%s/mcpservers", assistantID, projectID)
	_, resp, err := c.doRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	_, err = toObject(resp, &result)
	return result, err
}
