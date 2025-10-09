package osc

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/beevik/etree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetProjectMetaParam struct {
	ProjectName string `json:"project_name" jsonschema:"Name of the project"`
	Filter      string `json:"filter,omitempty" jsonschema:"Optional regexp to filter packages, returning all if empty"`
}

type Repository struct {
	Name           string   `json:"name" yaml:"name"`
	PathProject    string   `json:"path_project,omitempty" yaml:"path_project,omitempty"`
	PathRepository string   `json:"path_repository,omitempty" yaml:"path_repository,omitempty"`
	Arches         []string `json:"arches,omitempty" yaml:"arches,omitempty"`
}

type Package struct {
	Name   string            `json:"name"`
	Status map[string]string `json:"status,omitempty"`
}

type ProjectMeta struct {
	ProjectName  string       `json:"project_name"`
	Title        string       `json:"title,omitempty"`
	Description  string       `json:"description,omitempty"`
	Maintainers  []string     `json:"maintainers,omitempty"`
	Repositories []Repository `json:"repositories,omitempty"`
	Packages     []*Package   `json:"packages,omitempty"`
	SubProjects  []SubProject `json:"sub_projects,omitempty"`
	NumPackages  int          `json:"num_packages,omitempty"`
	NumFiltered  int          `json:"num_filtered,omitempty"`
}

type SubProject struct {
	Name string `json:"name"`
}

func (cred *OSCCredentials) listProjectPackages(ctx context.Context, projectName string) ([]*Package, error) {
	if projectName == "" {
		return nil, fmt.Errorf("project name cannot be empty")
	}

	apiURL, err := url.Parse(fmt.Sprintf("%s/source/%s", cred.GetAPiAddr(), projectName))
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
		return nil, ErrBundleOrProjectNotFound
	} else if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api request failed with status: %s", resp.Status)
	}

	doc := etree.NewDocument()
	if _, err := doc.ReadFrom(resp.Body); err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	dirElement := doc.SelectElement("directory")
	if dirElement == nil {
		return nil, fmt.Errorf("directory not found, project was: %s", projectName)
	}

	var packageNames []string
	for _, entry := range dirElement.SelectElements("entry") {
		if name := entry.SelectAttrValue("name", ""); name != "" {
			if !strings.HasPrefix(name, "_") {
				packageNames = append(packageNames, name)
			}
		}
	}

	packages := make([]*Package, len(packageNames))
	packageMap := make(map[string]*Package)
	for i, name := range packageNames {
		packages[i] = &Package{Name: name}
		packageMap[name] = packages[i]
	}

	// Get build results
	buildResultURL, err := url.Parse(fmt.Sprintf("%s/build/%s/_result", cred.GetAPiAddr(), projectName))
	if err != nil {
		return nil, fmt.Errorf("failed to parse build result API URL: %w", err)
	}

	req, err = http.NewRequestWithContext(ctx, "GET", buildResultURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request for build result: %w", err)
	}
	req.Header.Set("User-Agent", "osc-mcp")
	req.SetBasicAuth(cred.Name, cred.Passwd)
	req.Header.Set("Accept", "application/xml; charset=utf-8")

	resp, err = client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request for build result: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("failed to get build results", "project", projectName, "status", resp.Status)
		return packages, nil
	}

	buildDoc := etree.NewDocument()
	if _, err := buildDoc.ReadFrom(resp.Body); err != nil {
		slog.Warn("failed to parse build result", "project", projectName, "error", err)
		return packages, nil
	}

	resultList := buildDoc.SelectElement("resultlist")
	if resultList == nil {
		slog.Warn("no resultlist found in build result", "project", projectName)
		return packages, nil
	}

	for _, result := range resultList.SelectElements("result") {
		repo := result.SelectAttrValue("repository", "")
		arch := result.SelectAttrValue("arch", "")
		if repo == "" || arch == "" {
			continue
		}
		repoArch := fmt.Sprintf("%s/%s", repo, arch)
		for _, status := range result.SelectElements("status") {
			pkgNameWithFlavor := status.SelectAttrValue("package", "")
			code := status.SelectAttrValue("code", "")
			pkgName := pkgNameWithFlavor
			flavor := ""
			if strings.Contains(pkgNameWithFlavor, ":") {
				parts := strings.SplitN(pkgNameWithFlavor, ":", 2)
				pkgName = parts[0]
				flavor = parts[1]
			}

			if pkg, ok := packageMap[pkgName]; ok {
				if pkg.Status == nil {
					pkg.Status = make(map[string]string)
				}
				key := repoArch
				if flavor != "" {
					key = fmt.Sprintf("%s/%s", repoArch, flavor)
				}
				pkg.Status[key] = code
			}
		}
	}

	return packages, nil
}

func (cred *OSCCredentials) getProjectMetaInternal(ctx context.Context, projectName string) (*ProjectMeta, error) {
	if projectName == "" {
		return nil, fmt.Errorf("project name cannot be empty")
	}

	apiURL, err := url.Parse(fmt.Sprintf("%s/source/%s/_meta", cred.GetAPiAddr(), projectName))
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
		return nil, ErrBundleOrProjectNotFound
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
		ProjectName: projectElement.SelectAttrValue("name", ""),
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

func (cred *OSCCredentials) listAllProjects(ctx context.Context) ([]string, error) {
	apiURL, err := url.Parse(fmt.Sprintf("%s/source", cred.GetAPiAddr()))
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

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api request failed with status: %s", resp.Status)
	}

	doc := etree.NewDocument()
	if _, err := doc.ReadFrom(resp.Body); err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	dirElement := doc.SelectElement("directory")
	if dirElement == nil {
		return nil, fmt.Errorf("directory not found in response")
	}

	var projects []string
	for _, entry := range dirElement.SelectElements("entry") {
		if name := entry.SelectAttrValue("name", ""); name != "" {
			projects = append(projects, name)
		}
	}

	return projects, nil
}

func (cred *OSCCredentials) listSubProjects(ctx context.Context, projectName string) ([]SubProject, error) {
	allProjects, err := cred.listAllProjects(ctx)
	if err != nil {
		return nil, err
	}

	var subProjects []SubProject
	prefix := projectName + ":"
	for _, p := range allProjects {
		if strings.HasPrefix(p, prefix) {
			subProjectName := strings.TrimPrefix(p, prefix)
			if !strings.Contains(subProjectName, ":") {
				subProjects = append(subProjects, SubProject{Name: subProjectName})
			}
		}
	}
	return subProjects, nil
}

func (cred *OSCCredentials) GetProjectMeta(ctx context.Context, req *mcp.CallToolRequest, params GetProjectMetaParam) (*mcp.CallToolResult, *ProjectMeta, error) {
	slog.Debug("mcp tool call: GetProjectMeta", "params", params)
	res, err := cred.getProjectMetaInternal(ctx, params.ProjectName)
	if err != nil {
		return nil, nil, err
	}

	packages, err := cred.listProjectPackages(ctx, params.ProjectName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list packages for project %s: %w", params.ProjectName, err)
	}

	res.NumPackages = len(packages)

	if params.Filter != "" {
		re, err := regexp.Compile(params.Filter)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid filter regexp: %w", err)
		}
		var filteredPackages []*Package
		for _, pkg := range packages {
			if re.MatchString(pkg.Name) {
				filteredPackages = append(filteredPackages, pkg)
			}
		}
		res.Packages = filteredPackages
		res.NumFiltered = len(filteredPackages)
	} else {
		if len(packages) <= 100 {
			res.Packages = packages
		}
	}

	subProjects, err := cred.listSubProjects(ctx, params.ProjectName)
	if err != nil {
		slog.Warn("failed to list subprojects", "project", params.ProjectName, "error", err)
	} else {
		res.SubProjects = subProjects
	}

	return nil, res, nil
}

func (cred *OSCCredentials) setProjectMetaInternal(ctx context.Context, params ProjectMeta) error {
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

	apiURL, err := url.Parse(fmt.Sprintf("%s/source/%s/_meta", cred.GetAPiAddr(), params.ProjectName))
	if err != nil {
		return fmt.Errorf("failed to parse API URL: %w", err)
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
				Arches:      []string{"x86_64"},
			},
		}
	}

	if err := cred.setProjectMetaInternal(ctx, params); err != nil {
		return nil, nil, err
	}
	res, err := cred.getProjectMetaInternal(ctx, params.ProjectName)
	return nil, res, err
}
