package osc

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
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
	packages, err := cred.listLocalPackages()
	if err != nil {
		return nil, err
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

// listLocalPackages is the internal implementation that lists all packages
// that are locally checked out in the temporary directory.
// The expected directory structure is $tmpdir/$project/$package.
// It returns a slice of strings, where each string is a package in the
// format "project/package".
func (cred OSCCredentials) listLocalPackages() ([]string, error) {
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

	return packages, nil
}

// IsPackageCheckedOut checks if a package is checked out locally.
func (cred OSCCredentials) IsPackageCheckedOut(project, pkg string) (bool, error) {
	packagePath := filepath.Join(cred.TempDir, project, pkg)
	if _, err := os.Stat(packagePath); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("failed to check package directory %s: %w", packagePath, err)
	}

	return true, nil
}

// CheckOutPackage checks out a package to the temporary directory.
// It is a placeholder and does not perform an actual checkout.
func (cred OSCCredentials) CheckOutPackage(project, pkg string) error {
	packagePath := filepath.Join(cred.TempDir, project, pkg)
	if err := os.MkdirAll(packagePath, 0755); err != nil {
		return fmt.Errorf("failed to create package directory %s: %w", packagePath, err)
	}
	// In a real implementation, this would involve fetching files from OBS
	return nil
}

// GetPackageFiles returns the list of files for a given package.
func (cred OSCCredentials) GetPackageFiles(project, pkg string) ([]fs.DirEntry, error) {
	packagePath := filepath.Join(cred.TempDir, project, pkg)
	files, err := os.ReadDir(packagePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read package directory %s: %w", packagePath, err)
	}
	return files, nil
}