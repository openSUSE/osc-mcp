package osc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/jsonschema"
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

func GetBuildLogSchema() (*jsonschema.Schema, error) {
	schema, err := jsonschema.For[BuildLogParam]()
	if err != nil {
		return nil, err
	}
	schema.AdditionalProperties = &jsonschema.Schema{}
	schema.Default = []byte(`{
	"repository_name":"` + defRepo + `",
	"architecture_name":"` + defArch + `",
	"project_name":"",
	"package_name":"",
	"nr_lines":100,
	"show_succeded":false}`)
	return schema, nil
}

func (cred *OSCCredentials) BuildLog(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[BuildLogParam]) (*mcp.CallToolResultFor[any], error) {
	if params.Arguments.ProjectName == "" {
		return nil, fmt.Errorf("project name must be specified")
	}
	if params.Arguments.PackageName == "" {
		return nil, fmt.Errorf("package name must be specified")
	}
	if params.Arguments.RepositoryName == "" {
		//\FIXME Defaults from jsonschema doesn't seem to work
		params.Arguments.RepositoryName = defRepo
	}
	if params.Arguments.ArchitectureName == "" {
		//\FIXME Defaults from jsonschema doesn't seem to work
		params.Arguments.ArchitectureName = defArch
	}

	rawLog, err := cred.GetBuildLogRaw(ctx, params.Arguments.ProjectName, params.Arguments.RepositoryName, params.Arguments.ArchitectureName, params.Arguments.PackageName)
	if err != nil {
		return nil, err
	}
	log := buildlog.Parse(rawLog)
	jsonBytes, err := json.MarshalIndent(log.FormatJson(params.Arguments.NrLines, params.Arguments.ShowSucceded), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal json: %w", err)
	}

	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(jsonBytes),
			},
		},
	}, nil
}
