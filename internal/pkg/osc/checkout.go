package osc

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CheckoutPackageCmd struct {
	Project string `json:"project_name" jsonschema:"Name of the project"`
	Package string `json:"package_name" jsonschema:"Name of the package"`
}

type CheckoutPackageResult struct {
	Path        string `json:"path"`
	PackageName string `json:"package_name"`
	ProjectName string `json:"project_name"`
}

func (cred *OSCCredentials) CheckoutPackage(ctx context.Context, req *mcp.CallToolRequest, params CheckoutPackageCmd) (*mcp.CallToolResult, CheckoutPackageResult, error) {
	if params.Project == "" || params.Package == "" {
		return nil, CheckoutPackageResult{}, fmt.Errorf("project and package must be specified")
	}

	oscCmd := exec.CommandContext(ctx, "osc", "checkout", params.Project, params.Package)
	oscCmd.Dir = cred.TempDir
	var out bytes.Buffer
	oscCmd.Stdout = &out
	oscCmd.Stderr = &out
	if err := oscCmd.Run(); err != nil {
		slog.Error("failed to run osc checkout", slog.String("command", oscCmd.String()), slog.String("output", out.String()))
		return nil, CheckoutPackageResult{}, fmt.Errorf("failed to run osc checkout command `%s`: %w\nOutput:\n%s", oscCmd.String(), err, out.String())
	}

	return nil, CheckoutPackageResult{
		Path:        path.Join(cred.TempDir, params.Project, params.Package),
		PackageName: params.Package,
		ProjectName: params.Project,
	}, nil
}
