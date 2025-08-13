package osc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type BranchPackageParam struct {
	Project string `json:"project_name" jsonschema:"The project from which the package is branched."`
	Package string `json:"package_name" jsonschema:"The package that you want to branch"`
}

type BranchResult struct {
	TargetProject string `json:"target_project"`
	TargetPackage string `json:"target_package"`
	CheckoutDir   string `json:"checkout_dir"`
}

func (cred OSCCredentials) BranchPackage(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[BranchPackageParam]) (toolRes *mcp.CallToolResultFor[any], err error) {
	if params.Arguments.Project == "" {
		return nil, fmt.Errorf("project name cannot be empty")
	}
	if params.Arguments.Package == "" {
		return nil, fmt.Errorf("package name cannot be empty")
	}

	targetProject := fmt.Sprintf("home:%s:branches:%s", cred.Name, params.Arguments.Project)
	targetPackage := params.Arguments.Package

	apiURL, err := url.Parse(fmt.Sprintf("https://%s/source/%s/%s", cred.Apiaddr, params.Arguments.Project, params.Arguments.Package))
	if err != nil {
		return nil, fmt.Errorf("failed to parse API URL: %w", err)
	}
	q := apiURL.Query()
	q.Set("cmd", "branch")
	q.Set("target_project", targetProject)
	q.Set("target_package", targetPackage)
	apiURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(cred.Name, cred.Passwd)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api request failed with status: %s", resp.Status)
	}

	cmd := exec.CommandContext(ctx, "osc", "checkout", targetProject)
	cmd.Dir = cred.TempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to run '%s': %w\n%s", cmd.String(), err, string(output))
	}

	result := BranchResult{
		TargetProject: targetProject,
		TargetPackage: targetPackage,
		CheckoutDir:   filepath.Join(cred.TempDir, targetProject),
	}

	jsonBytes, err := json.MarshalIndent(result, "", "  ")
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
