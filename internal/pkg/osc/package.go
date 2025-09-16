package osc

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
)

type Defaults struct {
	Repositories    []Repository      `yaml:"repositories"`
	CopyrightHeader string            `yaml:"copyright_header"`
	Specs           map[string]string `yaml:"specs"`
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
	slog.Debug("mcp tool call: CreateBundle", "session", req.Session.ID(), "params", params)
	if params.PackageName == "" {
		return nil, CreateBundleResult{}, fmt.Errorf("package name cannot be empty")
	}

	projectName := params.ProjectName
	if projectName == "" {
		projectName = fmt.Sprintf("home:%s:osc-mpc:%s", cred.Name, params.Description)
	}

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

<<<<<<< HEAD
	if !meta.Exists {
		err = cred.createProject(ctx, projectName,
			fmt.Sprintf("Project for %s session %s", cred.Name, cred.SessionId),
			"Auto-generated project by osc-mcp.",
			defaults.Repositories,
		)
		if err != nil {
=======
	if len(meta.Maintainers) == 0 {
		title := params.Title
		if title == "" {
			title = fmt.Sprintf("Project for %s session %s", req.Session.ID())
		}
		description := params.Description
		if description == "" {
			description = "Auto-generated project by osc-mcp."
		}
		repositories := params.Repositories
		if len(repositories) == 0 {
			repositories = defaults.Repositories
		}
		if err := (&cred).setProjectMetaInternal(ctx, ProjectMeta{
			ProjectName:  projectName,
			Title:        title,
			Description:  description,
			Repositories: repositories,
			Maintainers:  []string{cred.Name},
		}); err != nil {
>>>>>>> 4ac734b (use seesion id instead of nanoId)
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
