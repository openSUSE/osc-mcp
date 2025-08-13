package osc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ListLocalPackagesParam is the parameter for the ListLocalPackages tool.
// It is empty because the tool does not require any parameters.
type ListLocalPackagesParam struct{}

// ListLocalPackages lists all packages that are locally checked out in the
// temporary directory.
// It is the tool implementation for the MCP.
func (cred OSCCredentials) ListLocalPackages(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ListLocalPackagesParam]) (toolRes *mcp.CallToolResultFor[any], err error) {
	var packages []string
	projectDirs, err := os.ReadDir(cred.TempDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read temp directory %s: %w", cred.TempDir, err)
	}

	for _, projectDir := range projectDirs {
		if !projectDir.IsDir() {
			continue
		}
		projectPath := filepath.Join(cred.TempDir, projectDir.Name())
		packageDirs, err := os.ReadDir(projectPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read project directory %s: %w", projectPath, err)
		}
		for _, packageDir := range packageDirs {
			if !packageDir.IsDir() {
				continue
			}
			packages = append(packages, projectDir.Name()+"/"+packageDir.Name())
		}
	}

	jsonBytes, err := json.MarshalIndent(packages, "", "  ")
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
