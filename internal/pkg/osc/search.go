package osc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/beevik/etree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchSrcPkgParam struct {
	Name     string   `json:"name" jsonschema:"Name of the source package to search"`
	Projects []string `json:"projects,omitempty" jsonschema:"Optional list of projects to search in"`
}

type PackageInfo struct {
	Name        string `json:"name"`
	Project     string `json:"project"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func (cred OSCCredentials) SearchSrcPkg(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[SearchSrcPkgParam]) (toolRes *mcp.CallToolResultFor[any], err error) {
	if params.Arguments.Name == "" {
		return nil, fmt.Errorf("package name to search cannot be empty")
	}

	match := fmt.Sprintf("@name='%s'", params.Arguments.Name)
	if len(params.Arguments.Projects) > 0 {
		var projectMatches []string
		for _, p := range params.Arguments.Projects {
			projectMatches = append(projectMatches, fmt.Sprintf("@project='%s'", p))
		}
		match = fmt.Sprintf("%s and (%s)", match, strings.Join(projectMatches, " or "))
	}

	apiURL, err := url.Parse(fmt.Sprintf("https://%s/search/package", cred.Apiaddr))
	if err != nil {
		return nil, fmt.Errorf("failed to parse API URL: %w", err)
	}
	q := apiURL.Query()
	q.Set("match", match)
	apiURL.RawQuery = q.Encode()

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

	var packages []PackageInfo
	for _, pkg := range doc.FindElements("//package") {
		p := PackageInfo{
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
