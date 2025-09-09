package osc

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/beevik/etree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ListSrcFilesParam struct {
	ProjectName string `json:"project_name" jsonschema:"Name of the project"`
	PackageName string `json:"package_name" jsonschema:"Name of the bundle or source package"`
	Local       bool   `json:"local" jsonschema:"List source files of local bundle"`
}

type FileInfo struct {
	Name  string `json:"name"`
	Size  string `json:"size"`
	MD5   string `json:"md5"`
	MTime string `json:"mtime"`
}

type ReturnedInfo struct {
	ProjectName string     `json:"project_name" jsonschema:"Name of the project"`
	PackageName string     `json:"package_name" jsonschema:"Name of the bundle or source package"`
	Files       []FileInfo `json:"files" jsonschema:"List of files"`
	Local       bool       `json:"local" jsonschema:"Is local package"`
}

func IgnoredDirs() []string {
	return []string{".osc", ".git"}
}

func (cred OSCCredentials) ListSrcFiles(ctx context.Context, req *mcp.CallToolRequest, params ListSrcFilesParam) (*mcp.CallToolResult, any, error) {
	if params.ProjectName == "" {
		return nil, nil, fmt.Errorf("project name cannot be empty")
	}
	if params.PackageName == "" {
		return nil, nil, fmt.Errorf("package name cannot be empty")
	}

	if params.Local {
		packagePath := filepath.Join(cred.TempDir, params.ProjectName, params.PackageName)
		entries, err := os.ReadDir(packagePath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read local package directory %s: %w", packagePath, err)
		}

		var files []FileInfo
		ignored := IgnoredDirs()

		for _, entry := range entries {
			isIgnored := false
			for _, ignoredDir := range ignored {
				if entry.Name() == ignoredDir {
					isIgnored = true
					break
				}
			}
			if isIgnored || entry.IsDir() {
				continue
			}

			filePath := filepath.Join(packagePath, entry.Name())
			info, err := entry.Info()
			if err != nil {
				continue
			}

			file, err := os.Open(filePath)
			if err != nil {
				continue
			}

			hash := md5.New()
			_, err = io.Copy(hash, file)
			file.Close()
			if err != nil {
				continue
			}
			md5sum := hex.EncodeToString(hash.Sum(nil))

			f := FileInfo{
				Name:  entry.Name(),
				Size:  fmt.Sprintf("%d", info.Size()),
				MD5:   md5sum,
				MTime: fmt.Sprintf("%d", info.ModTime().Unix()),
			}
			files = append(files, f)
		}

		return nil, ReturnedInfo{
			ProjectName: params.ProjectName,
			PackageName: params.PackageName,
			Files:       files,
			Local:       true,
		}, nil
	}

	apiURL, err := url.Parse(fmt.Sprintf("https://%s/source/%s/%s", cred.Apiaddr, params.ProjectName, params.PackageName))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse API URL: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", apiURL.String(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.SetBasicAuth(cred.Name, cred.Passwd)
	httpReq.Header.Set("Accept", "application/xml; charset=utf-8")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("api request failed with status: %s", resp.Status)
	}

	doc := etree.NewDocument()
	if _, err := doc.ReadFrom(resp.Body); err != nil {
		return nil, nil, fmt.Errorf("failed to read response body: %w", err)
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

	return nil, ReturnedInfo{
		ProjectName: params.ProjectName,
		PackageName: params.PackageName,
		Files:       files,
		Local:       false,
	}, nil
}

type ListLocalParams struct {
	Number int `json:"number,omitempty" jsonschema:"number of packages to display"`
}

type LocalPackage struct {
	PackageName string `json:"package_name"`
	ProjectName string `json:"project_name"`
	Path        string `json:"path"`
}
