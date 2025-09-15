package osc

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/beevik/etree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
)

type Defaults struct {
	Repositories    []Repository      `yaml:"repositories"`
	CopyrightHeader string            `yaml:"copyright_header"`
	Specs           map[string]string `yaml:"specs"`
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

	req.Header.Set("User-Agent", "osc-mcp")
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

type CreateBundleParam struct {
	PackageName string `json:"package_name" jsonschema:"The name of the package to create."`
	Flavor      string `json:"flavor,omitempty" jsonschema:"The flavor of the package (e.g., python, go, java, lua, c, cpp). Determines the generated spec file."`
	ProvideSpec bool   `json:"provide_spec,omitempty" jsonschema:"Provide a spec file"`
}

type CreateBundleResult struct {
	Project string `json:"project"`
	Package string `json:"package"`
	Path    string `json:"path"`
}

func (cred OSCCredentials) CreateBundle(ctx context.Context, req *mcp.CallToolRequest, params CreateBundleParam) (*mcp.CallToolResult, CreateBundleResult, error) {
	slog.Debug("mcp tool call: CreateBundle", "params", params)
	if params.PackageName == "" {
		return nil, CreateBundleResult{}, fmt.Errorf("package name cannot be empty")
	}

	projectName := fmt.Sprintf("home:%s:osc-mpc:%s", cred.Name, cred.SessionId)

	meta, err := cred.getProjectMetaInternal(ctx, projectName)
	if err != nil {
		return nil, CreateBundleResult{}, fmt.Errorf("failed to check if project exists: %w", err)
	}

	var defaults Defaults
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, CreateBundleResult{}, fmt.Errorf("could not get user home directory: %w", err)
	}
	configPath := filepath.Join(home, ".config", "osc-mcp", "defaults.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		configPath = "defaults.yaml"
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			return nil, CreateBundleResult{}, fmt.Errorf("defaults.yaml not found in ~/.config/osc-mcp/ or current directory")
		}
	}

	yamlFile, err := os.ReadFile(configPath)
	if err != nil {
		return nil, CreateBundleResult{}, fmt.Errorf("failed to read defaults.yaml: %w", err)
	}

	err = yaml.Unmarshal(yamlFile, &defaults)
	if err != nil {
		return nil, CreateBundleResult{}, fmt.Errorf("failed to unmarshal defaults.yaml: %w", err)
	}

	if !meta.Exists {
		err = cred.createProject(ctx, projectName,
			fmt.Sprintf("Project for %s session %s", cred.Name, cred.SessionId),
			"Auto-generated project by osc-mcp.",
			defaults.Repositories,
		)
		if err != nil {
			return nil, CreateBundleResult{}, fmt.Errorf("failed to create project: %w", err)
		}
	}

	cmd := exec.CommandContext(ctx, "osc", "checkout", projectName)
	cmd.Dir = cred.TempDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, CreateBundleResult{}, fmt.Errorf("failed to run '%s': %w\n%s", cmd.String(), err, string(output))
	}

	projectDir := filepath.Join(cred.TempDir, projectName)

	cmd = exec.CommandContext(ctx, "osc", "mkpac", params.PackageName)
	cmd.Dir = projectDir
	output, err = cmd.CombinedOutput()
	if err != nil {
		return nil, CreateBundleResult{}, fmt.Errorf("failed to run '%s': %w\n%s", cmd.String(), err, string(output))
	}
	if params.ProvideSpec {
		flavor := params.Flavor
		if flavor == "" || flavor == "c" || flavor == "cpp" {
			flavor = "default"
		}

		specTemplate, ok := defaults.Specs[flavor]
		if !ok {
			specTemplate, ok = defaults.Specs["default"]
			if !ok {
				return nil, CreateBundleResult{}, fmt.Errorf("no spec template for flavor '%s' and no default spec found in defaults.yaml", params.Flavor)
			}
		}

		fullSpecTemplate := defaults.CopyrightHeader + specTemplate
		specContent := strings.ReplaceAll(fullSpecTemplate, "__PACKAGE_NAME__", params.PackageName)
		specContent = strings.ReplaceAll(specContent, "__YEAR__", fmt.Sprintf("%d", time.Now().Year()))

		packageDir := filepath.Join(projectDir, params.PackageName)
		specFilePath := filepath.Join(packageDir, params.PackageName+".spec")

		err = os.WriteFile(specFilePath, []byte(specContent), 0644)
		if err != nil {
			return nil, CreateBundleResult{}, fmt.Errorf("failed to write spec file: %w", err)
		}
	}
	return nil, CreateBundleResult{
		Project: projectName,
		Package: params.PackageName,
		Path:    filepath.Join(projectDir, params.PackageName),
	}, nil
}
