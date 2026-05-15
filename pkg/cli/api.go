package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gptscript-ai/cmd"
	"github.com/obot-platform/obot/apiclient"
	apiTypes "github.com/obot-platform/obot/apiclient/types"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/duration"
	"sigs.k8s.io/yaml"
)

type API struct {
	root *Obot `usage:"-"`
}

func (a *API) Customize(cmd *cobra.Command) {
	cmd.Use = "api"
	cmd.Short = "Read Obot API resources"
	cmd.Args = cobra.NoArgs
}

func (a *API) Run(cmd *cobra.Command, _ []string) error {
	return cmd.Help()
}

type APIGet struct {
	root        *Obot  `usage:"-"`
	Output      string `usage:"Output format: table, json, yaml" default:"table" local:"true"`
	AssistantID string `usage:"Assistant ID for project-scoped resources" local:"true"`
	ProjectID   string `usage:"Project ID for project-scoped resources" local:"true"`
	CatalogID   string `usage:"Catalog ID for catalog-scoped MCP server resources" local:"true"`
	WorkspaceID string `usage:"Workspace ID for workspace-scoped MCP server resources" local:"true"`
}

func (a *APIGet) Customize(cmd *cobra.Command) {
	cmd.Use = "get RESOURCE [ID]"
	cmd.Short = "Get one or more Obot API resources"
	cmd.Args = cobra.RangeArgs(1, 2)
}

func (a *APIGet) Run(cmd *cobra.Command, args []string) error {
	resource, err := parseAPIResource(args[0], a.AssistantID, a.ProjectID, a.CatalogID, a.WorkspaceID)
	if err != nil {
		return err
	}

	if len(args) == 2 {
		obj, err := resource.get(cmd.Context(), a.root.Client, args[1])
		if err != nil {
			return err
		}
		return outputAPIObject(a.Output, obj, resource.tableRows(obj), true)
	}

	obj, err := resource.list(cmd.Context(), a.root.Client)
	if err != nil {
		return err
	}
	return outputAPIObject(a.Output, obj, resource.tableRows(obj), true)
}

type APIDescribe struct {
	root        *Obot  `usage:"-"`
	Output      string `usage:"Output format: yaml or json" default:"yaml" local:"true"`
	AssistantID string `usage:"Assistant ID for project-scoped resources" local:"true"`
	ProjectID   string `usage:"Project ID for project-scoped resources" local:"true"`
	CatalogID   string `usage:"Catalog ID for catalog-scoped MCP server resources" local:"true"`
	WorkspaceID string `usage:"Workspace ID for workspace-scoped MCP server resources" local:"true"`
}

func (a *APIDescribe) Customize(cmd *cobra.Command) {
	cmd.Use = "describe RESOURCE ID"
	cmd.Short = "Describe an Obot API resource"
	cmd.Args = cobra.ExactArgs(2)
}

func (a *APIDescribe) Run(cmd *cobra.Command, args []string) error {
	resource, err := parseAPIResource(args[0], a.AssistantID, a.ProjectID, a.CatalogID, a.WorkspaceID)
	if err != nil {
		return err
	}

	obj, err := resource.get(cmd.Context(), a.root.Client, args[1])
	if err != nil {
		return err
	}
	return outputAPIObject(a.Output, obj, nil, false)
}

type apiResource struct {
	list      func(context.Context, *apiclient.Client) (any, error)
	get       func(context.Context, *apiclient.Client, string) (any, error)
	tableRows func(any) [][]string
}

func parseAPIResource(name, assistantID, projectID, catalogID, workspaceID string) (*apiResource, error) {
	name = strings.ToLower(strings.ReplaceAll(name, "-", ""))

	switch name {
	case "projects", "project":
		if assistantID != "" || projectID != "" || catalogID != "" || workspaceID != "" {
			return nil, fmt.Errorf("projects do not support assistant/project/catalog/workspace scoping flags")
		}
		return &apiResource{
			list: func(ctx context.Context, c *apiclient.Client) (any, error) {
				return c.ListProjects(ctx, apiclient.ListProjectsOptions{})
			},
			get: func(ctx context.Context, c *apiclient.Client, id string) (any, error) {
				return c.GetProject(ctx, id)
			},
			tableRows: projectRows,
		}, nil
	case "threads", "thread":
		if projectID != "" || catalogID != "" || workspaceID != "" {
			return nil, fmt.Errorf("threads only support top-level listing or --assistant-id filtering")
		}
		return &apiResource{
			list: func(ctx context.Context, c *apiclient.Client) (any, error) {
				return c.ListThreads(ctx, apiclient.ListThreadsOptions{AgentID: assistantID})
			},
			get: func(ctx context.Context, c *apiclient.Client, id string) (any, error) {
				return c.GetThread(ctx, id)
			},
			tableRows: threadRows,
		}, nil
	case "mcpservers", "mcpserver":
		if (assistantID == "") != (projectID == "") {
			return nil, fmt.Errorf("project-scoped mcpservers require both --assistant-id and --project-id")
		}
		if countNonEmpty(catalogID, workspaceID) > 1 {
			return nil, fmt.Errorf("choose only one mcpserver scope: --catalog-id or --workspace-id")
		}
		if assistantID != "" && (catalogID != "" || workspaceID != "") {
			return nil, fmt.Errorf("project-scoped mcpservers cannot also use catalog or workspace scope")
		}
		if assistantID != "" {
			return &apiResource{
				list: func(ctx context.Context, c *apiclient.Client) (any, error) {
					return c.ListProjectMCPServers(ctx, assistantID, projectID)
				},
				get: func(ctx context.Context, c *apiclient.Client, id string) (any, error) {
					return c.GetProjectMCPServer(ctx, assistantID, projectID, id)
				},
				tableRows: projectMCPServerRows,
			}, nil
		}
		return &apiResource{
			list: func(ctx context.Context, c *apiclient.Client) (any, error) {
				return c.ListMCPServers(ctx, apiclient.ListMCPServersOptions{
					CatalogID:   catalogID,
					WorkspaceID: workspaceID,
				})
			},
			get: func(ctx context.Context, c *apiclient.Client, id string) (any, error) {
				return c.GetMCPServer(ctx, id, apiclient.ListMCPServersOptions{
					CatalogID:   catalogID,
					WorkspaceID: workspaceID,
				})
			},
			tableRows: mcpServerRows,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported resource %q; supported resources: projects, threads, mcpservers", name)
	}
}

func outputAPIObject(output string, obj any, rows [][]string, allowTable bool) error {
	switch strings.ToLower(output) {
	case "", "table":
		if !allowTable {
			return fmt.Errorf("table output is only supported for get")
		}
		printTable(rows)
		return nil
	case "json":
		data, err := json.MarshalIndent(obj, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(os.Stdout, string(data))
		return err
	case "yaml":
		jsonBytes, err := json.Marshal(obj)
		if err != nil {
			return err
		}
		yamlBytes, err := yaml.JSONToYAML(jsonBytes)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(yamlBytes)
		if err == nil && len(yamlBytes) > 0 && yamlBytes[len(yamlBytes)-1] != '\n' {
			_, err = fmt.Fprintln(os.Stdout)
		}
		return err
	default:
		return fmt.Errorf("unsupported output format %q", output)
	}
}

func printTable(rows [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		_, _ = fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	_ = w.Flush()
}

func projectRows(obj any) [][]string {
	rows := [][]string{{"NAME", "ID", "ASSISTANT", "AGE"}}
	switch v := obj.(type) {
	case apiTypes.ProjectList:
		for _, item := range v.Items {
			rows = append(rows, []string{item.Name, item.ID, item.AssistantID, age(item.Created)})
		}
	case *apiTypes.Project:
		rows = append(rows, []string{v.Name, v.ID, v.AssistantID, age(v.Created)})
	}
	return rows
}

func threadRows(obj any) [][]string {
	rows := [][]string{{"NAME", "ID", "STATE", "PROJECT", "AGE"}}
	switch v := obj.(type) {
	case apiTypes.ThreadList:
		for _, item := range v.Items {
			rows = append(rows, []string{item.Name, item.ID, item.State, item.ProjectID, age(item.Created)})
		}
	case *apiTypes.Thread:
		rows = append(rows, []string{v.Name, v.ID, v.State, v.ProjectID, age(v.Created)})
	}
	return rows
}

func mcpServerRows(obj any) [][]string {
	rows := [][]string{{"NAME", "ID", "ALIAS", "CONFIGURED", "STATUS", "AGE"}}
	switch v := obj.(type) {
	case apiTypes.MCPServerList:
		for _, item := range v.Items {
			rows = append(rows, []string{item.MCPServerManifest.Name, item.ID, item.Alias, fmt.Sprint(item.Configured), item.DeploymentStatus, age(item.Created)})
		}
	case *apiTypes.MCPServer:
		rows = append(rows, []string{v.MCPServerManifest.Name, v.ID, v.Alias, fmt.Sprint(v.Configured), v.DeploymentStatus, age(v.Created)})
	}
	return rows
}

func projectMCPServerRows(obj any) [][]string {
	rows := [][]string{{"NAME", "ID", "ALIAS", "CONFIGURED", "AGE"}}
	switch v := obj.(type) {
	case apiTypes.ProjectMCPServerList:
		for _, item := range v.Items {
			rows = append(rows, []string{item.Name, item.ID, item.Alias, fmt.Sprint(item.Configured), age(item.Created)})
		}
	case *apiTypes.ProjectMCPServer:
		rows = append(rows, []string{v.Name, v.ID, v.Alias, fmt.Sprint(v.Configured), age(v.Created)})
	}
	return rows
}

func age(t apiTypes.Time) string {
	if t.Time.IsZero() {
		return ""
	}
	return duration.HumanDuration(time.Since(t.Time))
}

func countNonEmpty(values ...string) int {
	count := 0
	for _, value := range values {
		if value != "" {
			count++
		}
	}
	return count
}

func newAPICommand(root *Obot) *cobra.Command {
	return cmd.Command(&API{root: root},
		&APIGet{root: root},
		&APIDescribe{root: root},
	)
}
