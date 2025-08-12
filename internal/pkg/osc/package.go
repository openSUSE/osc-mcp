package osc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/beevik/etree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
)

type CreatePackageParam struct {
	PackageName string `json:"package_name" jsonschema:"The name of the package to create."`
}

type DefaultRepositories struct {
	Repositories []Repository `yaml:"repositories"`
}

// projectExists checks if a project exists on the build service.
func (cred OSCCredentials) projectExists(ctx context.Context, projectName string) (bool, error) {
	apiURL, err := url.Parse(fmt.Sprintf("https://%s/source/%s/_meta", cred.Apiaddr, projectName))
	if err != nil {
		return false, fmt.Errorf("failed to parse API URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL.String(), nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(cred.Name, cred.Passwd)
	req.Header.Set("Accept", "application/xml; charset=utf-8")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}

	return false, fmt.Errorf("api request failed with status: %s", resp.Status)
}

func (cred OSCCredentials) createProject(ctx context.Context, projectName string, title string, description string, repositories []Repository) error {
	doc := etree.NewDocument()
	doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)
	project := doc.CreateElement("project")
	project.CreateAttr("name", projectName)

	if title != "" {
		project.CreateElement("title").SetText(title)
	}
	if description != "" {
		project.CreateElement("description").SetText(description)
	}

	person := project.CreateElement("person")
	person.CreateAttr("userid", cred.Name)
	person.CreateAttr("role", "maintainer")

	for _, repo := range repositories {
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

	apiURL, err := url.Parse(fmt.Sprintf("https://%s/source/%s/_meta", cred.Apiaddr, projectName))
	if err != nil {
		return fmt.Errorf("failed to parse API URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", apiURL.String(), strings.NewReader(metaString))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(cred.Name, cred.Passwd)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Accept", "application/xml; charset=utf-8")

	client := &http.Client{}
	resp, err := client.Do(req)
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

func (cred OSCCredentials) CreatePackage(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[CreatePackageParam]) (toolRes *mcp.CallToolResultFor[any], err error) {
	if params.Arguments.PackageName == "" {
		return nil, fmt.Errorf("package name cannot be empty")
	}

	projectName := fmt.Sprintf("home:%s:%s", cred.Name, cred.SessionId)

	exists, err := cred.projectExists(ctx, projectName)
	if err != nil {
		return nil, fmt.Errorf("failed to check if project exists: %w", err)
	}

	if !exists {
		var defaults DefaultRepositories
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("could not get user home directory: %w", err)
		}
		configPath := filepath.Join(home, ".config", "osc-mcp", "defaults.yaml")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			configPath = "defaults.yaml"
			if _, err := os.Stat(configPath); os.IsNotExist(err) {
				return nil, fmt.Errorf("defaults.yaml not found in ~/.config/osc-mcp/ or current directory")
			}
		}

		yamlFile, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read defaults.yaml: %w", err)
		}

		err = yaml.Unmarshal(yamlFile, &defaults)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal defaults.yaml: %w", err)
		}

		err = cred.createProject(ctx, projectName,
			fmt.Sprintf("Project for %s session %s", cred.Name, cred.SessionId),
			"Auto-generated project by osc-mcp.",
			defaults.Repositories,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create project: %w", err)
		}
	}

	cmd := exec.CommandContext(ctx, "osc", "mkpac", projectName, params.Arguments.PackageName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to run 'osc mkpac': %w\n%s", err, string(output))
	}

	result := struct {
		Project string `json:"project"`
		Package string `json:"package"`
	}{
		Project: projectName,
		Package: params.Arguments.PackageName,
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
