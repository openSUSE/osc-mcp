package osc

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/beevik/etree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetProjectMetaParam struct {
	ProjectName string `json:"project_name" jsonschema:"Name of the project"`
}

type Repository struct {
	Name           string   `json:"name" yaml:"name"`
	PathProject    string   `json:"path_project,omitempty" yaml:"path_project,omitempty"`
	PathRepository string   `json:"path_repository,omitempty" yaml:"path_repository,omitempty"`
	Arches         []string `json:"arches,omitempty" yaml:"arches,omitempty"`
}

type ProjectMeta struct {
	Name         string       `json:"name"`
	Exists       bool         `json:"exists"`
	Title        string       `json:"title,omitempty"`
	Description  string       `json:"description,omitempty"`
	Maintainers  []string     `json:"maintainers,omitempty"`
	Repositories []Repository `json:"repositories,omitempty"`
}

func (cred *OSCCredentials) getProjectMetaInternal(ctx context.Context, projectName string) (*ProjectMeta, error) {
	if projectName == "" {
		return nil, fmt.Errorf("project name cannot be empty")
	}

	apiURL, err := url.Parse(fmt.Sprintf("https://%s/source/%s/_meta", cred.Apiaddr, projectName))
	if err != nil {
		return nil, fmt.Errorf("failed to parse API URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "osc-mcp")
	req.SetBasicAuth(cred.Name, cred.Passwd)
	req.Header.Set("Accept", "application/xml; charset=utf-8")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &ProjectMeta{
			Name:   projectName,
			Exists: false,
		}, nil
	} else if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api request failed with status: %s", resp.Status)
	}

	doc := etree.NewDocument()
	if _, err := doc.ReadFrom(resp.Body); err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	projectElement := doc.SelectElement("project")
	if projectElement == nil {
		return nil, fmt.Errorf("project not found, name was: %s", projectName)
	}

	meta := &ProjectMeta{
		Name: projectElement.SelectAttrValue("name", ""),
	}

	if title := projectElement.SelectElement("title"); title != nil {
		meta.Title = title.Text()
	}
	if description := projectElement.SelectElement("description"); description != nil {
		meta.Description = description.Text()
	}

	for _, person := range projectElement.SelectElements("person") {
		if role := person.SelectAttrValue("role", ""); role == "maintainer" {
			meta.Maintainers = append(meta.Maintainers, person.SelectAttrValue("userid", ""))
		}
	}

	for _, repo := range projectElement.SelectElements("repository") {
		r := Repository{
			Name: repo.SelectAttrValue("name", ""),
		}
		if path := repo.SelectElement("path"); path != nil {
			r.PathProject = path.SelectAttrValue("project", "")
			r.PathRepository = path.SelectAttrValue("repository", "")
		}
		for _, arch := range repo.SelectElements("arch") {
			r.Arches = append(r.Arches, arch.Text())
		}
		meta.Repositories = append(meta.Repositories, r)
	}

	return meta, nil
}

func (cred *OSCCredentials) GetProjectMeta(ctx context.Context, req *mcp.CallToolRequest, params GetProjectMetaParam) (*mcp.CallToolResult, *ProjectMeta, error) {
	slog.Debug("mcp tool call: GetProjectMeta", "params", params)
	res, err := cred.getProjectMetaInternal(ctx, params.ProjectName)
	return nil, res, err
}

type SetProjectMetaParam struct {
	ProjectName  string       `json:"project_name" jsonschema:"Name of the project"`
	Comment      string       `json:"comment,omitempty" jsonschema:"Comment that explains the changes you made in meta file."`
	Title        string       `json:"title,omitempty" jsonschema:"The title of the project."`
	Description  string       `json:"description,omitempty" jsonschema:"The description of the project."`
	Maintainers  []string     `json:"maintainers,omitempty" jsonschema:"List of user IDs for project maintainers."`
	Repositories []Repository `json:"repositories" jsonschema:"List of repositories for the project."`
}

func (cred *OSCCredentials) SetProjectMeta(ctx context.Context, req *mcp.CallToolRequest, params SetProjectMetaParam) (*mcp.CallToolResult, *ProjectMeta, error) {
	slog.Debug("mcp tool call: SetProjectMeta", "params", params)
	if params.ProjectName == "" {
		return nil, nil, fmt.Errorf("project name cannot be empty")
	}
	if len(params.Repositories) == 0 {
		return nil, nil, fmt.Errorf("at least one repository must be provided")

	doc := etree.NewDocument()
	doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)
	project := doc.CreateElement("project")
	project.CreateAttr("name", params.ProjectName)

	if params.Title != "" {
		project.CreateElement("title").SetText(params.Title)
	}
	if params.Description != "" {
		project.CreateElement("description").SetText(params.Description)
	}

	for _, maintainer := range params.Maintainers {
		person := project.CreateElement("person")
		person.CreateAttr("userid", maintainer)
		person.CreateAttr("role", "maintainer")
	}

	for _, repo := range params.Repositories {
		repository := project.CreateElement("repository")
		repository.CreateAttr("name", repo.Name)
		if repo.PathProject != "" {
			path := repository.CreateElement("path")
			path.CreateAttr("project", repo.PathProject)
			if repo.PathRepository != "" {
				path.CreateAttr("repository", repo.PathRepository)
			}
		}
		for _, arch := range repo.Arches {
			repository.CreateElement("arch").SetText(arch)
		}
	}

	doc.Indent(2)
	metaString, err := doc.WriteToString()
	if err != nil {
		return fmt.Errorf("failed to generate XML: %w", err)
	}

	apiURL, err := url.Parse(fmt.Sprintf("https://%s/source/%s/_meta", cred.Apiaddr, params.ProjectName))
	if err != nil {
		return fmt.Errorf("failed to parse API URL: %w", err)
	}

	if params.Comment != "" {
		q := apiURL.Query()
		q.Set("comment", params.Comment)
		apiURL.RawQuery = q.Encode()
	}

	httpReq, err := http.NewRequestWithContext(ctx, "PUT", apiURL.String(), strings.NewReader(metaString))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("User-Agent", "osc-mcp")
	httpReq.SetBasicAuth(cred.Name, cred.Passwd)
	httpReq.Header.Set("Content-Type", "application/xml; charset=utf-8")
	httpReq.Header.Set("Accept", "application/xml; charset=utf-8")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("api request failed with status: %s\nbody:\n%s", resp.Status, string(body))
	}
	return nil
}

func (cred *OSCCredentials) SetProjectMeta(ctx context.Context, req *mcp.CallToolRequest, params ProjectMeta) (*mcp.CallToolResult, *ProjectMeta, error) {
	slog.Debug("mcp tool call: SetProjectMeta", "params", params)
	if params.ProjectName == "" {
		return nil, nil, fmt.Errorf("project name cannot be empty")
	}
	if len(params.Repositories) == 0 {
		params.Repositories = []Repository{
			{
				Name:        "openSUSE_Tumbleweed",
				PathProject: "openSUSE:Factory",
				Arches:      []string{"x86_64", "i586"},
			},
		}
	}

	if err := cred.setProjectMetaInternal(ctx, params); err != nil {
		return nil, nil, err
	}
	res, err := cred.getProjectMetaInternal(ctx, params.ProjectName)
	return nil, res, err
}
