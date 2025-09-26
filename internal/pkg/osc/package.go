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
	Services        map[string]string `yaml:"services"`
}

type CreateBundleParam struct {
	PackageName  string       `json:"package_name" jsonschema:"The name of the package to create."`
	Flavor       string       `json:"flavor,omitempty"`
	Service      []string     `json:"service,omitempty" jsonschema:"The services to create a _service file for."`
	ProjectName  string       `json:"project_name,omitempty" jsonschema:"Name of the project. If not provided, a project name is generated."`
	Title        string       `json:"title,omitempty" jsonschema:"The title of the project."`
	Description  string       `json:"description,omitempty" jsonschema:"The description of the project."`
	Repositories []Repository `json:"repositories,omitempty" jsonschema:"List of repositories for the project."`
	Overwrite    bool         `json:"overwrite,omitempty" jsonschema:"If true, overwrite existing files."`
}

type CreateBundleResult struct {
	Project        string            `json:"project"`
	Package        string            `json:"package"`
	Path           string            `json:"path"`
	GeneratedFiles map[string]string `json:"generated_files,omitempty"`
}

func ReadDefaults() (Defaults, error) {
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

func (cred OSCCredentials) Create(ctx context.Context, req *mcp.CallToolRequest, params CreateBundleParam) (*mcp.CallToolResult, any, error) {
	slog.Debug("mcp tool call: Create", "session", req.Session.ID(), "params", params)
	if params.PackageName == "" {
		return nil, nil, fmt.Errorf("package name cannot be empty")
	}

	projectName := params.ProjectName
	if projectName == "" {
		projectName = fmt.Sprintf("home:%s:osc-mpc:%s", cred.Name, req.Session.ID())
	}
	defaults, err := ReadDefaults()
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
	if _, err := os.Stat(filepath.Join(cred.TempDir, projectName, params.PackageName)); err != nil {

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

	}
	projectDir := filepath.Join(cred.TempDir, projectName)
	result := CreateBundleResult{
		Project:        projectName,
		Package:        params.PackageName,
		Path:           filepath.Join(projectDir, params.PackageName),
		GeneratedFiles: make(map[string]string),
	}
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

		if _, err := os.Stat(specFilePath); err == nil && !params.Overwrite {
			return nil, nil, fmt.Errorf("spec file '%s' already exists. Use overwrite option to force.", specFilePath)
		} else if err != nil && !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("failed to check spec file existence: %w", err)
		}

		err = os.WriteFile(specFilePath, []byte(specContent), 0644)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to write spec file: %w", err)
		}
		result.GeneratedFiles[specFilePath] = specContent
	}

	if len(params.Service) > 0 {
		var serviceContents []string
		for _, serviceName := range params.Service {
			serviceTemplate, ok := defaults.Services[serviceName]
			if !ok {
				return nil, nil, fmt.Errorf("no service template for '%s' found in defaults.yaml", serviceName)
			}
			serviceContents = append(serviceContents, strings.ReplaceAll(serviceTemplate, "__PACKAGE_NAME__", params.PackageName))
		}

		packageDir := filepath.Join(projectDir, params.PackageName)
		serviceFilePath := filepath.Join(packageDir, "_service")

		if _, err := os.Stat(serviceFilePath); err == nil && !params.Overwrite {
			return nil, nil, fmt.Errorf("service file '%s' already exists. Use overwrite option to force.", serviceFilePath)
		} else if err != nil && !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("failed to check service file existence: %w", err)
		}

		finalServiceContent := "<services>\n" + strings.Join(serviceContents, "\n") + "\n</services>"
		err = os.WriteFile(serviceFilePath, []byte(finalServiceContent), 0644)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to write _service file: %w", err)
		}
		result.GeneratedFiles[serviceFilePath] = finalServiceContent
	}

	return nil, result, nil
}

func CreateBundleInputSchema() *jsonschema.Schema {
	defaults, err := ReadDefaults()
	var flavors []any
	var services []any
	if err != nil {
		slog.Warn("could not read defaults for creating input schema", "err", err)
	} else {
		for k := range defaults.Specs {
			flavors = append(flavors, k)
		}
		for k := range defaults.Services {
			services = append(services, k)
		}
	}
	inputSchema, _ := jsonschema.For[CreateBundleParam](nil)
	inputSchema.Properties["flavor"].Enum = flavors
	inputSchema.Properties["flavor"].Description = "The flavor of the bundle so that a spec with proper defaults for this flavor is generated."
	inputSchema.Properties["service"].Description = "The services to create a _service file for."
	inputSchema.Properties["service"].Items.Enum = services
	inputSchema.Properties["overwrite"].Description = "If true, overwrite existing files."

	return inputSchema
}
