package osc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/beevik/etree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type DeleteProjectParam struct {
	ProjectName string `json:"project_name,omitempty" jsonschema:"The project to be deleted. Defaults to home:$USERNAME:$SESSIONID if not provided."`
	Force       bool   `json:"force,omitempty" jsonschema:"Set to true to delete the project even if other projects link to it."`
	Comment     string `json:"comment,omitempty" jsonschema:"A comment explaining the reason for the deletion."`
}

type DeleteProjectResult struct {
	Message string `json:"message"`
}

func (cred OSCCredentials) DeleteProject(ctx context.Context, req *mcp.CallToolRequest, params DeleteProjectParam) (*mcp.CallToolResult, DeleteProjectResult, error) {
	projectName := params.ProjectName
	if projectName == "" {
		projectName = fmt.Sprintf("home:%s:%s", cred.Name, cred.SessionId)
	}

	apiURL, err := url.Parse(fmt.Sprintf("https://%s/source/%s", cred.Apiaddr, projectName))
	if err != nil {
		return nil, DeleteProjectResult{}, fmt.Errorf("failed to parse API URL: %w", err)
	}

	q := apiURL.Query()
	if params.Force {
		q.Set("force", "1")
	}
	if params.Comment != "" {
		q.Set("comment", params.Comment)
	}
	apiURL.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", apiURL.String(), nil)
	if err != nil {
		return nil, DeleteProjectResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.SetBasicAuth(cred.Name, cred.Passwd)
	httpReq.Header.Set("Accept", "application/xml; charset=utf-8")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, DeleteProjectResult{}, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, DeleteProjectResult{}, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, DeleteProjectResult{}, fmt.Errorf("api request failed with status: %s\nbody:\n%s", resp.Status, string(body))
	}

	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(body); err != nil {
		return nil, DeleteProjectResult{}, fmt.Errorf("failed to parse response xml: %w", err)
	}

	status := doc.SelectElement("status")
	summary := status.SelectElement("summary")

	return nil, DeleteProjectResult{
		Message: fmt.Sprintf("Project '%s' deleted successfully: %s", projectName, summary.Text()),
	}, nil
}
