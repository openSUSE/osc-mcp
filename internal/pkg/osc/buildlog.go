package osc

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openSUSE/osc-mcp/internal/pkg/buildlog"
)

func (cred *OSCCredentials) getFromApi(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.SetBasicAuth(cred.Name, cred.Passwd)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	return bodyBytes, resp.StatusCode, nil
}

type BuildStatus struct {
	XMLName xml.Name `xml:"status"`
	Code    string   `xml:"code,attr"`
	Details string   `xml:"details"`
}

// GetBuildStatus retrieves the build status for a given package.
func (cred *OSCCredentials) GetBuildStatus(ctx context.Context, projectName, repositoryName, architectureName, packageName string) (*BuildStatus, error) {
	url := fmt.Sprintf("https://%s/build/%s/%s/%s/%s/_status", cred.Apiaddr, projectName, repositoryName, architectureName, packageName)
	body, statusCode, err := cred.getFromApi(ctx, url)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get build status: status code %d, body: %s", statusCode, string(body))
	}

	var status BuildStatus
	if err := xml.Unmarshal(body, &status); err != nil {
		return nil, fmt.Errorf("failed to parse build status XML: %w", err)
	}
	return &status, nil
}

type BuildDepInfo struct {
	XMLName  xml.Name  `xml:"builddepinfo"`
	Packages []Package `xml:"package"`
}

type Package struct {
	XMLName xml.Name `xml:"package"`
	Name    string   `xml:"name,attr"`
	Deps    []Dep    `xml:"dep"`
}

type Dep struct {
	XMLName xml.Name `xml:"dep"`
	Name    string   `xml:"name,attr"`
	State   string   `xml:"state,attr"`
}

// GetBuildDepInfo retrieves the build dependency information for a project.
func (cred *OSCCredentials) GetBuildDepInfo(ctx context.Context, projectName, repositoryName, architectureName string) (*BuildDepInfo, error) {
	url := fmt.Sprintf("https://%s/build/%s/%s/%s/_builddepinfo", cred.Apiaddr, projectName, repositoryName, architectureName)
	body, statusCode, err := cred.getFromApi(ctx, url)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get build dependency info: status code %d, body: %s", statusCode, string(body))
	}

	var depInfo BuildDepInfo
	if err := xml.Unmarshal(body, &depInfo); err != nil {
		return nil, fmt.Errorf("failed to parse build dependency info XML: %w", err)
	}
	return &depInfo, nil
}

// GetBuildLogRaw retrieves the build log for a given package and returns the raw content.
func (cred *OSCCredentials) GetBuildLogRaw(ctx context.Context, projectName, repositoryName, architectureName, packageName string) (string, error) {
	url := fmt.Sprintf("https://%s/build/%s/%s/%s/%s/_log", cred.Apiaddr, projectName, repositoryName, architectureName, packageName)
	bodyBytes, statusCode, err := cred.getFromApi(ctx, url)
	if err != nil {
		return "", err
	}

	if statusCode == http.StatusOK {
		return string(bodyBytes), nil
	}

	if statusCode != http.StatusNotFound {
		return "", fmt.Errorf("failed to get build log: status code %d, body: %s", statusCode, string(bodyBytes))
	}

	// Status is 404, check build status
	status, err := cred.GetBuildStatus(ctx, projectName, repositoryName, architectureName, packageName)
	if err != nil {
		return "", fmt.Errorf("failed to get build log (status code %d), and also failed to get build status: %w", statusCode, err)
	}

	if status.Code != "unresolvable" {
		return "", fmt.Errorf("failed to get build log: status code %d, build status is '%s': %s", statusCode, status.Code, status.Details)
	}

	// It's unresolvable. Now get dep info.
	depInfo, err := cred.GetBuildDepInfo(ctx, projectName, repositoryName, architectureName)
	if err != nil {
		return "", fmt.Errorf("package is unresolvable, but failed to get dependency info: %w", err)
	}

	var missingDeps []string
	for _, p := range depInfo.Packages {
		if p.Name == packageName {
			for _, d := range p.Deps {
				if d.State == "missing" {
					missingDeps = append(missingDeps, d.Name)
				}
			}
			break
		}
	}

	if len(missingDeps) > 0 {
		return "", fmt.Errorf("package is unresolvable. Missing dependencies: %s", strings.Join(missingDeps, ", "))
	}

	return "", fmt.Errorf("package is unresolvable, but could not determine missing dependencies. Details: %s", status.Details)
}

type BuildLogParam struct {
	ProjectName      string `json:"project_name" jsonschema:"Name of the project"`
	PackageName      string `json:"package_name" jsonschema:"Name of the package"`
	RepositoryName   string `json:"repository_name,omitempty" jsonschema:"Repository name"`
	ArchitectureName string `json:"architecture_name,omitempty" jsonschema:"Architecture name"`
	NrLines          int    `json:"nr_lines,omitempty" jsonschema:"Maximum number of lines"`
	ShowSucceeded     bool   `json:"show_succeeded,omitempty" jsonschema:"Also show succeeded logs"`
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
	return nil, log.FormatJson(params.NrLines, params.ShowSucceeded), nil
}