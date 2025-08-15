package osc

import (
	"bytes"
	"context"
	"encoding/json"
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

func (cred *OSCCredentials) CheckoutPackage(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[CheckoutPackageCmd]) (*mcp.CallToolResultFor[any], error) {
	if params.Arguments.Project == "" || params.Arguments.Package == "" {
		return nil, fmt.Errorf("project and package must be specified")
	}

	oscCmd := exec.CommandContext(ctx, "osc", "checkout", params.Arguments.Project, params.Arguments.Package)
	oscCmd.Dir = cred.TempDir
	var out bytes.Buffer
	oscCmd.Stdout = &out
	oscCmd.Stderr = &out
	if err := oscCmd.Run(); err != nil {
		slog.Error("failed to run osc checkout", slog.String("command", oscCmd.String()), slog.String("output", out.String()))
		return nil, fmt.Errorf("failed to run osc checkout command `%s`: %w\nOutput:\n%s", oscCmd.String(), err, out.String())
	}

	result := map[string]string{
		"path":          path.Join(cred.TempDir, params.Arguments.Project, params.Arguments.Package),
		"package_name":  params.Arguments.Package,
		"project_name:": params.Arguments.Project,
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
