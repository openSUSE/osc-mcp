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

func (cred OSCCredentials) DeleteProject(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[DeleteProjectParam]) (toolRes *mcp.CallToolResultFor[any], err error) {
	projectName := params.Arguments.ProjectName
	if projectName == "" {
		projectName = fmt.Sprintf("home:%s:%s", cred.Name, cred.SessionId)
	}

	apiURL, err := url.Parse(fmt.Sprintf("https://%s/source/%s", cred.Apiaddr, projectName))
	if err != nil {
		return nil, fmt.Errorf("failed to parse API URL: %w", err)
	}

	q := apiURL.Query()
	if params.Arguments.Force {
		q.Set("force", "1")
	}
	if params.Arguments.Comment != "" {
		q.Set("comment", params.Arguments.Comment)
	}
	apiURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "DELETE", apiURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(cred.Name, cred.Passwd)
	req.Header.Set("Accept", "application/xml; charset=utf-8")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api request failed with status: %s\nbody:\n%s", resp.Status, string(body))
	}

	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(body); err != nil {
		return nil, fmt.Errorf("failed to parse response xml: %w", err)
	}

	status := doc.SelectElement("status")
	summary := status.SelectElement("summary")

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("Project '%s' deleted successfully: %s", projectName, summary.Text()),
			},
		},
	}, nil
}
