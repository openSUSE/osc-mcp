package osc

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/beevik/etree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchSrcBundleParam struct {
	Name     string   `json:"package_name" jsonschema:"Name of the source package to search"`
	Projects []string `json:"projects,omitempty" jsonschema:"Optional list of projects to search in"`
}

type BundleInfo struct {
	Name        string `json:"name"`
	Project     string `json:"project"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func (cred OSCCredentials) SearchSrcBundle(ctx context.Context, req *mcp.CallToolRequest, params SearchSrcBundleParam) (*mcp.CallToolResult, []BundleInfo, error) {
	if params.Name == "" {
		return nil, nil, fmt.Errorf("package name to search cannot be empty")
	}

	match := fmt.Sprintf("@name='%s'", params.Name)
	if len(params.Projects) > 0 {
		var projectMatches []string
		for _, p := range params.Projects {
			projectMatches = append(projectMatches, fmt.Sprintf("@project='%s'", p))
		}
		match = fmt.Sprintf("%s and (%s)", match, strings.Join(projectMatches, " or "))
	}

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

	return nil, packages, nil
}
