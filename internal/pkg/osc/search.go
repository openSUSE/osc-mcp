package osc

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/beevik/etree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchSrcBundleParam struct {
	Name     string   `json:"package_name,omitempty" jsonschema:"Name of the source package to search"`
	Projects []string `json:"projects,omitempty" jsonschema:"Optional list of projects to search in"`
}

type BundleInfo struct {
	Name        string `json:"name"`
	Project     string `json:"project"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type BundleOut struct {
	Result []BundleInfo `json:"result" jsonschema:"List of found bundles."`
}

func listLocalPackages(path string, packageName string) ([]BundleInfo, error) {
	var bundles []BundleInfo
	err := filepath.WalkDir(path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasSuffix(path, "/.osc") {
			prjDir := filepath.Dir(path)
			prjName := filepath.Base(prjDir)
			if packageName != "" && prjName != packageName {
				return nil
			}
			bundles = append(bundles, BundleInfo{Project: prjDir, Name: prjName})
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return bundles, nil
}

func (cred OSCCredentials) SearchSrcBundle(ctx context.Context, req *mcp.CallToolRequest, params SearchSrcBundleParam) (*mcp.CallToolResult, any, error) {
	isLocal := false
	if len(params.Projects) == 1 && strings.EqualFold("local", strings.ToLower(params.Projects[0])) || (len(params.Projects) == 0 && params.Name == "") {
		isLocal = true
	}
	if isLocal {
		var bundles []BundleInfo
		bundles, err := listLocalPackages(cred.TempDir, params.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to list local packages in '%s': %w", cred.TempDir, err)
		}
		return nil, BundleOut{Result: bundles}, nil
	}

	var matches []string
	if params.Name != "" {
		matches = append(matches, fmt.Sprintf("@name='%s'", params.Name))
	}
	if len(params.Projects) > 0 {
		var projectMatches []string
		for _, p := range params.Projects {
			projectMatches = append(projectMatches, fmt.Sprintf("@project='%s'", p))
		}
		matches = append(matches, fmt.Sprintf("(%s)", strings.Join(projectMatches, " or ")))
	}
	match := strings.Join(matches, " and ")

	apiURL, err := url.Parse(fmt.Sprintf("https://%s/search/package", cred.Apiaddr))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse API URL: %w", err)
	}
	q := apiURL.Query()
	q.Set("match", match)
	apiURL.RawQuery = q.Encode()

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

	var packages []BundleInfo
	for _, pkg := range doc.FindElements("//package") {
		p := BundleInfo{
			Name:    pkg.SelectAttrValue("name", ""),
			Project: pkg.SelectAttrValue("project", ""),
		}
		if title := pkg.SelectElement("title"); title != nil {
			p.Title = title.Text()
		}
		if description := pkg.SelectElement("description"); description != nil {
			p.Description = description.Text()
		}
		packages = append(packages, p)
	}

	return nil, BundleOut{
		Result: packages,
	}, nil
}
