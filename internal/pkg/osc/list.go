package osc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"

	"github.com/beevik/etree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListSrcFilesParam struct {
	ProjectName string `json:"project_name" jsonschema:"Name of the project"`
	PackageName string `json:"package_name" jsonschema:"Name of the package"`
}

type FileInfo struct {
	Name  string `json:"name"`
	Size  string `json:"size"`
	MD5   string `json:"md5"`
	MTime string `json:"mtime"`
}

func IgnoredDirs() []string {
	return []string{".osc", ".git"}
}

func (cred OSCCredentials) ListSrcFiles(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ListSrcFilesParam]) (toolRes *mcp.CallToolResultFor[any], err error) {
	if params.Arguments.ProjectName == "" {
		return nil, fmt.Errorf("project name cannot be empty")
	}
	if params.Arguments.PackageName == "" {
		return nil, fmt.Errorf("package name cannot be empty")
	}

	apiURL, err := url.Parse(fmt.Sprintf("https://%s/source/%s/%s", cred.Apiaddr, params.Arguments.ProjectName, params.Arguments.PackageName))
	if err != nil {
		return nil, fmt.Errorf("failed to parse API URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL.String(), nil)
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

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api request failed with status: %s", resp.Status)
	}

	doc := etree.NewDocument()
	if _, err := doc.ReadFrom(resp.Body); err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var files []FileInfo
	for _, entry := range doc.FindElements("//entry") {
		f := FileInfo{
			Name:  entry.SelectAttrValue("name", ""),
			Size:  entry.SelectAttrValue("size", ""),
			MD5:   entry.SelectAttrValue("md5", ""),
			MTime: entry.SelectAttrValue("mtime", ""),
		}
		files = append(files, f)
	}

	jsonBytes, err := json.MarshalIndent(files, "", "  ")
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

type ListLocalParams struct {
	Number int `json:"number,omitempty" jsonschema:"number of packages to display"`
}

func (cred OSCCredentials) ListLocalPackages(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ListLocalParams]) (toolRes *mcp.CallToolResultFor[any], err error) {
	type local_package struct {
		package_name string
		project_name string
		path         string
	}
	packages := []local_package{}
	projectDirs, err := os.ReadDir(cred.TempDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read temp directory %s: %w", cred.TempDir, err)
	}

	for _, projectDir := range projectDirs {
		if !projectDir.IsDir() {
			continue
		}
		if slices.Contains(IgnoredDirs(), projectDir.Name()) {
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
			if slices.Contains(IgnoredDirs(), packageDir.Name()) {
				continue
			}
			packages = append(packages, local_package{
				package_name: projectDir.Name(),
				project_name: packageDir.Name(),
				path:         packageDir.Name(),
			})
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
