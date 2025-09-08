package osc

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openSUSE/osc-mcp/internal/pkg/buildlog"
)

// GetBuildLogRaw retrieves the build log for a given package and returns the raw content.
func (cred *OSCCredentials) GetBuildLogRaw(ctx context.Context, projectName, repositoryName, architectureName, packageName string) (string, error) {
	url := fmt.Sprintf("https://%s/build/%s/%s/%s/%s/_log", cred.Apiaddr, projectName, repositoryName, architectureName, packageName)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)

	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(cred.Name, cred.Passwd)

	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return "", fmt.Errorf("failed to execute request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("failed to get build log, status code %d, and failed to read body: %w", resp.StatusCode, err)
		}
		return "", fmt.Errorf("failed to get build log: status code %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	bodyBytes, err := io.ReadAll(resp.Body)

	if err != nil {
		return "", fmt.Errorf("failed to read build log response body: %w", err)
	}

	return string(bodyBytes), nil
}

type BuildLogParam struct {
	ProjectName      string `json:"project_name" jsonschema:"Name of the project"`
	PackageName      string `json:"package_name" jsonschema:"Name of the package"`
	RepositoryName   string `json:"repository_name,omitempty" jsonschema:"Repository name"`
	ArchitectureName string `json:"architecture_name,omitempty" jsonschema:"Architecture name"`
	NrLines          int    `json:"nr_lines,omitempty" jsonschema:"Maximum number of lines"`
	ShowSucceded     bool   `json:"show_succeded,omitempty" jsonschema:"Also show succeded logs"`
}

const defRepo = "openSUSE_Tumbleweed"
const defArch = "x86_64"

func (cred *OSCCredentials) BuildLog(ctx context.Context, req *mcp.CallToolRequest, params BuildLogParam) (*mcp.CallToolResult, map[string]any, error) {
	if params.ProjectName == "" {
		return nil, nil, fmt.Errorf("project name must be specified")
	}
	if params.PackageName == "" {
		return nil, nil, fmt.Errorf("package name must be specified")
	}
	if params.RepositoryName == "" {
		params.RepositoryName = defRepo
	}
	if params.ArchitectureName == "" {
		params.ArchitectureName = defArch
	}

	rawLog, err := cred.GetBuildLogRaw(ctx, params.ProjectName, params.RepositoryName, params.ArchitectureName, params.PackageName)
	if err != nil {
		return nil, nil, err
	}
	log := buildlog.Parse(rawLog)
	return nil, log.FormatJson(params.NrLines, params.ShowSucceded), nil
}
