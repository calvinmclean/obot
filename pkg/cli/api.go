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
	AssistantID string `usage:"Assistant ID for assistant-scoped or project-scoped resources" local:"true"`
	ProjectID   string `usage:"Project ID for project-scoped resources" local:"true"`
	ThreadID    string `usage:"Thread ID for thread-scoped resources" local:"true"`
	CatalogID   string `usage:"Catalog ID for catalog-scoped MCP server resources" local:"true"`
	WorkspaceID string `usage:"Workspace ID for workspace-scoped MCP server resources" local:"true"`
	ToolType    string `usage:"Tool reference type filter" local:"true"`
}

func (a *APIGet) Customize(cmd *cobra.Command) {
	cmd.Use = "get RESOURCE [ID]"
	cmd.Short = "Get one or more Obot API resources"
	cmd.Args = cobra.RangeArgs(1, 2)
}

func (a *APIGet) Run(cmd *cobra.Command, args []string) error {
	resource, err := parseAPIResource(args[0], a.AssistantID, a.ProjectID, a.ThreadID, a.CatalogID, a.WorkspaceID, a.ToolType)
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
	AssistantID string `usage:"Assistant ID for assistant-scoped or project-scoped resources" local:"true"`
	ProjectID   string `usage:"Project ID for project-scoped resources" local:"true"`
	ThreadID    string `usage:"Thread ID for thread-scoped resources" local:"true"`
	CatalogID   string `usage:"Catalog ID for catalog-scoped MCP server resources" local:"true"`
	WorkspaceID string `usage:"Workspace ID for workspace-scoped MCP server resources" local:"true"`
	ToolType    string `usage:"Tool reference type filter" local:"true"`
}

func (a *APIDescribe) Customize(cmd *cobra.Command) {
	cmd.Use = "describe RESOURCE ID"
	cmd.Short = "Describe an Obot API resource"
	cmd.Args = cobra.ExactArgs(2)
}

func (a *APIDescribe) Run(cmd *cobra.Command, args []string) error {
	resource, err := parseAPIResource(args[0], a.AssistantID, a.ProjectID, a.ThreadID, a.CatalogID, a.WorkspaceID, a.ToolType)
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

func parseAPIResource(name, assistantID, projectID, threadID, catalogID, workspaceID, toolType string) (*apiResource, error) {
	name = strings.ToLower(strings.ReplaceAll(name, "-", ""))

	scopeErr := func(resource, supported string) error {
		return fmt.Errorf("%s only support %s", resource, supported)
	}

	scopeCount := countNonEmpty(assistantID, projectID, threadID, catalogID, workspaceID, toolType)

	switch name {
	case "projects", "project":
		if scopeCount > 0 {
			return nil, scopeErr("projects", "top-level access")
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
		if projectID != "" || threadID != "" || catalogID != "" || workspaceID != "" || toolType != "" {
			return nil, scopeErr("threads", "top-level access or --assistant-id filtering")
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
		if threadID != "" || toolType != "" {
			return nil, scopeErr("mcpservers", "top-level access, --catalog-id, --workspace-id, or both --assistant-id and --project-id")
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
	case "agents", "agent":
		if scopeCount > 0 {
			return nil, scopeErr("agents", "top-level access")
		}
		return &apiResource{
			list: func(ctx context.Context, c *apiclient.Client) (any, error) {
				return c.ListAgents(ctx, apiclient.ListAgentsOptions{})
			},
			get: func(ctx context.Context, c *apiclient.Client, id string) (any, error) {
				return c.GetAgent(ctx, id)
			},
			tableRows: agentRows,
		}, nil
	case "runs", "run":
		if projectID != "" || catalogID != "" || workspaceID != "" || toolType != "" {
			return nil, scopeErr("runs", "top-level access, --assistant-id filtering, or --thread-id filtering")
		}
		if assistantID != "" && threadID != "" {
			return nil, fmt.Errorf("choose only one runs scope: --assistant-id or --thread-id")
		}
		return &apiResource{
			list: func(ctx context.Context, c *apiclient.Client) (any, error) {
				return c.ListRuns(ctx, apiclient.ListRunsOptions{AgentID: assistantID, ThreadID: threadID})
			},
			get: func(ctx context.Context, c *apiclient.Client, id string) (any, error) {
				return c.GetRun(ctx, id)
			},
			tableRows: runRows,
		}, nil
	case "workflows", "workflow":
		if assistantID != "" || projectID != "" || catalogID != "" || workspaceID != "" || toolType != "" {
			return nil, scopeErr("workflows", "top-level access or --thread-id filtering")
		}
		return &apiResource{
			list: func(ctx context.Context, c *apiclient.Client) (any, error) {
				return c.ListWorkflows(ctx, apiclient.ListWorkflowsOptions{ThreadID: threadID})
			},
			get: func(ctx context.Context, c *apiclient.Client, id string) (any, error) {
				return c.GetWorkflow(ctx, id)
			},
			tableRows: workflowRows,
		}, nil
	case "tasks", "task":
		if catalogID != "" || workspaceID != "" || toolType != "" {
			return nil, scopeErr("tasks", "top-level access, --thread-id scope, or both --assistant-id and --project-id scope")
		}
		if assistantID != "" && projectID == "" {
			return nil, fmt.Errorf("project-scoped tasks require both --assistant-id and --project-id")
		}
		if assistantID == "" && projectID != "" {
			return nil, fmt.Errorf("--project-id requires --assistant-id for tasks")
		}
		if threadID != "" && (assistantID != "" || projectID != "") {
			return nil, fmt.Errorf("tasks cannot combine --thread-id with project scope")
		}
		if assistantID != "" {
			return &apiResource{
				list: func(ctx context.Context, c *apiclient.Client) (any, error) {
					return c.ListProjectTasks(ctx, assistantID, projectID)
				},
				get: func(ctx context.Context, c *apiclient.Client, id string) (any, error) {
					return c.GetProjectTask(ctx, assistantID, projectID, id)
				},
				tableRows: taskRows,
			}, nil
		}
		return &apiResource{
			list: func(ctx context.Context, c *apiclient.Client) (any, error) {
				return c.ListTasks(ctx, apiclient.ListTasksOptions{ThreadID: threadID})
			},
			get: func(ctx context.Context, c *apiclient.Client, id string) (any, error) {
				return c.GetTask(ctx, id, apiclient.ListTasksOptions{ThreadID: threadID})
			},
			tableRows: taskRows,
		}, nil
	case "webhooks", "webhook":
		if scopeCount > 0 {
			return nil, scopeErr("webhooks", "top-level access")
		}
		return &apiResource{
			list: func(ctx context.Context, c *apiclient.Client) (any, error) {
				return c.ListWebhooks(ctx)
			},
			get: func(ctx context.Context, c *apiclient.Client, id string) (any, error) {
				return c.GetWebhook(ctx, id)
			},
			tableRows: webhookRows,
		}, nil
	case "toolreferences", "toolreference":
		if assistantID != "" || projectID != "" || threadID != "" || catalogID != "" || workspaceID != "" {
			return nil, scopeErr("tool-references", "top-level access or --tool-type filtering")
		}
		return &apiResource{
			list: func(ctx context.Context, c *apiclient.Client) (any, error) {
				return c.ListToolReferences(ctx, apiclient.ListToolReferencesOptions{ToolType: apiTypes.ToolReferenceType(toolType)})
			},
			get: func(ctx context.Context, c *apiclient.Client, id string) (any, error) {
				return c.GetToolReference(ctx, id)
			},
			tableRows: toolReferenceRows,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported resource %q; supported resources: projects, threads, mcpservers, agents, runs, workflows, tasks, webhooks, tool-references", name)
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

func agentRows(obj any) [][]string {
	rows := [][]string{{"NAME", "ID", "ALIAS", "DEFAULT MODEL", "AGE"}}
	switch v := obj.(type) {
	case apiTypes.AgentList:
		for _, item := range v.Items {
			rows = append(rows, []string{item.Name, item.ID, item.Alias, item.Model, age(item.Created)})
		}
	case *apiTypes.Agent:
		rows = append(rows, []string{v.Name, v.ID, v.Alias, v.Model, age(v.Created)})
	}
	return rows
}

func runRows(obj any) [][]string {
	rows := [][]string{{"ID", "STATE", "THREAD", "ASSISTANT", "WORKFLOW", "AGE"}}
	switch v := obj.(type) {
	case apiTypes.RunList:
		for _, item := range v.Items {
			rows = append(rows, []string{item.ID, item.State, item.ThreadID, item.AgentID, item.WorkflowID, age(item.Created)})
		}
	case *apiTypes.Run:
		rows = append(rows, []string{v.ID, v.State, v.ThreadID, v.AgentID, v.WorkflowID, age(v.Created)})
	}
	return rows
}

func workflowRows(obj any) [][]string {
	rows := [][]string{{"NAME", "ID", "ALIAS", "THREAD", "AGE"}}
	switch v := obj.(type) {
	case apiTypes.WorkflowList:
		for _, item := range v.Items {
			rows = append(rows, []string{item.Name, item.ID, item.Alias, item.ThreadID, age(item.Created)})
		}
	case *apiTypes.Workflow:
		rows = append(rows, []string{v.Name, v.ID, v.Alias, v.ThreadID, age(v.Created)})
	}
	return rows
}

func taskRows(obj any) [][]string {
	rows := [][]string{{"NAME", "ID", "ALIAS", "PROJECT", "MANAGED", "AGE"}}
	switch v := obj.(type) {
	case apiTypes.TaskList:
		for _, item := range v.Items {
			rows = append(rows, []string{item.Name, item.ID, item.Alias, item.ProjectID, fmt.Sprint(item.Managed), age(item.Created)})
		}
	case *apiTypes.Task:
		rows = append(rows, []string{v.Name, v.ID, v.Alias, v.ProjectID, fmt.Sprint(v.Managed), age(v.Created)})
	}
	return rows
}

func webhookRows(obj any) [][]string {
	rows := [][]string{{"NAME", "ID", "ALIAS", "WORKFLOW", "AGE"}}
	switch v := obj.(type) {
	case apiTypes.WebhookList:
		for _, item := range v.Items {
			rows = append(rows, []string{item.Name, item.ID, item.Alias, item.WorkflowName, age(item.Created)})
		}
	case *apiTypes.Webhook:
		rows = append(rows, []string{v.Name, v.ID, v.Alias, v.WorkflowName, age(v.Created)})
	}
	return rows
}

func toolReferenceRows(obj any) [][]string {
	rows := [][]string{{"NAME", "ID", "TYPE", "ACTIVE", "RESOLVED", "AGE"}}
	switch v := obj.(type) {
	case apiTypes.ToolReferenceList:
		for _, item := range v.Items {
			rows = append(rows, []string{item.Name, item.ID, string(item.ToolType), fmt.Sprint(item.Active), fmt.Sprint(item.Resolved), age(item.Created)})
		}
	case *apiTypes.ToolReference:
		rows = append(rows, []string{v.Name, v.ID, string(v.ToolType), fmt.Sprint(v.Active), fmt.Sprint(v.Resolved), age(v.Created)})
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
