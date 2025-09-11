package osc

import (
	"context"
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

func (cred OSCCredentials) BranchBundle(ctx context.Context, req *mcp.CallToolRequest, params BranchPackageParam) (*mcp.CallToolResult, BranchResult, error) {
	if params.Project == "" {
		return nil, BranchResult{}, fmt.Errorf("project name cannot be empty")
	}
	if params.Package == "" {
		return nil, BranchResult{}, fmt.Errorf("package name cannot be empty")
	}

	targetProject := fmt.Sprintf("home:%s:branches:%s", cred.Name, params.Project)
	targetPackage := params.Package

	apiURL, err := url.Parse(fmt.Sprintf("https://%s/source/%s/%s", cred.Apiaddr, params.Project, params.Package))
	if err != nil {
		return nil, BranchResult{}, fmt.Errorf("failed to parse API URL: %w", err)
	}
	q := apiURL.Query()
	q.Set("cmd", "branch")
	q.Set("target_project", targetProject)
	q.Set("target_package", targetPackage)
	apiURL.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL.String(), nil)
	if err != nil {
		return nil, BranchResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.SetBasicAuth(cred.Name, cred.Passwd)

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, BranchResult{}, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, BranchResult{}, fmt.Errorf("api request failed with status: %s", resp.Status)
	}

	cmd := exec.CommandContext(ctx, "osc", "checkout", targetProject)
	cmd.Dir = cred.TempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, BranchResult{}, fmt.Errorf("failed to run '%s': %w\n%s", cmd.String(), err, string(output))
	}

	result := BranchResult{
		TargetProject: targetProject,
		TargetPackage: targetPackage,
		CheckoutDir:   filepath.Join(cred.TempDir, targetProject),
	}

	return nil, result, nil
}
