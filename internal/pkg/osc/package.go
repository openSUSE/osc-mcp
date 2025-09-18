package osc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
)

var defaultsYaml []byte

func SetDefaultsYaml(data []byte) {
	defaultsYaml = data
}

type Defaults struct {
	Repositories    []Repository      `yaml:"repositories"`
	CopyrightHeader string            `yaml:"copyright_header"`
	Specs           map[string]string `yaml:"specs"`
}

type CreateBundleParam struct {
	PackageName  string       `json:"package_name" jsonschema:"The name of the package to create."`
	Flavor       string       `json:"flavor,omitempty"`
	ProjectName  string       `json:"project_name,omitempty" jsonschema:"Name of the project. If not provided, a project name is generated."`
	Title        string       `json:"title,omitempty" jsonschema:"The title of the project."`
	Description  string       `json:"description,omitempty" jsonschema:"The description of the project."`
	Repositories []Repository `json:"repositories,omitempty" jsonschema:"List of repositories for the project."`
}

type CreateBundleResult struct {
	Project string `json:"project"`
	Package string `json:"package"`
	Path    string `json:"path"`
}

func readDefaults() (Defaults, error) {
	var defaults Defaults
	var yamlFile []byte
	var err error

	configPaths := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		configPaths = append(configPaths, filepath.Join(home, ".config", "osc-mcp", "defaults.yaml"))
	} else {
		slog.Warn("could not get user home directory, skipping user config", "err", err)
	}
	configPaths = append(configPaths, "/etc/osc-mcp/defaults.yaml", "/usr/etc/osc-mcp/defaults.yaml")

	var found bool
	for _, configPath := range configPaths {
		if _, err := os.Stat(configPath); err == nil {
			yamlFile, err = os.ReadFile(configPath)
			if err != nil {
				return Defaults{}, fmt.Errorf("failed to read %s: %w", configPath, err)
			}
			slog.Debug("using defaults from", "path", configPath)
			found = true
			break
		}
	}

	if !found {
		slog.Debug("using embedded defaults")
		yamlFile = defaultsYaml
	}

	err = yaml.Unmarshal(yamlFile, &defaults)
	if err != nil {
		return Defaults{}, fmt.Errorf("failed to unmarshal defaults.yaml: %w", err)
	}
	return defaults, nil
}

func (cred OSCCredentials) CreateBundle(ctx context.Context, req *mcp.CallToolRequest, params CreateBundleParam) (*mcp.CallToolResult, any, error) {
	slog.Debug("mcp tool call: CreateBundle", "session", req.Session.ID(), "params", params)
	if params.PackageName == "" {
		return nil, nil, fmt.Errorf("package name cannot be empty")
	}

	projectName := params.ProjectName
	if projectName == "" {
		projectName = fmt.Sprintf("home:%s:osc-mpc:%s", cred.Name, req.Session.ID())
	}
	defaults, err := readDefaults()
	if err != nil {
		return nil, nil, err
	}

	_, err = cred.getProjectMetaInternal(ctx, projectName)
	if errors.Is(err, ErrBundleOrProjectNotFound) {
		title := params.Title
		if title == "" {
			title = fmt.Sprintf("Project for %s session %s", cred.Name, req.Session.ID())
		}
		description := params.Description
		if description == "" {
			description = "Auto-generated project by osc-mcp."
		}
		repositories := params.Repositories
		if len(repositories) == 0 {
			repositories = defaults.Repositories
		}
		if err := cred.setProjectMetaInternal(ctx, ProjectMeta{
			ProjectName:  projectName,
			Title:        title,
			Description:  description,
			Repositories: repositories,
			Maintainers:  []string{cred.Name},
		}); err != nil {
			return nil, nil, fmt.Errorf("failed to create project: %w", err)
		}
	} else if err != nil {
		return nil, nil, fmt.Errorf("failed to check if project exists: %w", err)
	}
	// now check if bundle allreay exists

	if bundles, err := cred.searchRemoteSrcBundle(ctx, params.PackageName, []string{projectName}); err != nil {
		return nil, nil, err
	} else if len(bundles) > 0 {
		return nil, nil, fmt.Errorf("Bundle %s allreay exists in project %s", params.PackageName, projectName)
	}
	createBdlCmd := []string{"osc", "rmkpac", projectName, params.PackageName}
	cmd := exec.CommandContext(ctx, createBdlCmd[0], createBdlCmd[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to run '%s': %w\n%s", cmd.String(), err, string(output))
	}

	checkOutCmd := []string{"osc", "checkout", projectName, params.PackageName}
	cmd = exec.CommandContext(ctx, checkOutCmd[0], checkOutCmd[1:]...)
	cmd.Dir = cred.TempDir
	output, err = cmd.CombinedOutput()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to run '%s': %w\n%s", cmd.String(), err, string(output))
	}

	projectDir := filepath.Join(cred.TempDir, projectName)

	if params.Flavor != "" {
		flavor := params.Flavor
		if flavor == "c" || flavor == "cpp" {
			flavor = "default"
		}

		specTemplate, ok := defaults.Specs[flavor]
		if !ok {
			specTemplate, ok = defaults.Specs["default"]
			if !ok {
				return nil, nil, fmt.Errorf("no spec template for flavor '%s' and no default spec found in defaults.yaml", params.Flavor)
			}
		}

		fullSpecTemplate := defaults.CopyrightHeader + specTemplate
		specContent := strings.ReplaceAll(fullSpecTemplate, "__PACKAGE_NAME__", params.PackageName)
		specContent = strings.ReplaceAll(specContent, "__YEAR__", fmt.Sprintf("%d", time.Now().Year()))

		packageDir := filepath.Join(projectDir, params.PackageName)
		specFilePath := filepath.Join(packageDir, params.PackageName+".spec")

		err = os.WriteFile(specFilePath, []byte(specContent), 0644)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to write spec file: %w", err)
		}
	}
	return nil, CreateBundleResult{
		Project: projectName,
		Package: params.PackageName,
		Path:    filepath.Join(projectDir, params.PackageName),
	}, nil
}

func CreateBundleInputSchema() *jsonschema.Schema {
	defaults, err := readDefaults()
	var flavors []any
	if err != nil {
		slog.Warn("could not read defaults for creating input schema", "err", err)
	} else {
		for k := range defaults.Specs {
			flavors = append(flavors, k)
		}
	}
	inputSchema, _ := jsonschema.For[CreateBundleParam](nil)
	inputSchema.Properties["flavor"].Enum = flavors
	inputSchema.Properties["flavor"].Description = "The flavor of the bundle so that a spec with proper defaults for this flavor is generated."

	return inputSchema
}
