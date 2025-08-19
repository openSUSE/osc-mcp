package osc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openSUSE/osc-mcp/internal/pkg/buildlog"
)

type BuildParam struct {
	ProjectName       string `json:"project_name" jsonschema:"Name of the project"`
	PackageName       string `json:"package_name" jsonschema:"Name of the package"`
	VmType            string `json:"vm_type,omitempty" jsonschema:"VM type to use for build (e.g., chroot, kvm, podman, docker)"`
	MultibuildPackage string `json:"multibuild_package,omitempty" jsonschema:"Specify the flavor of a multibuild package"`
	Distribution      string `json:"distribution,omitempty" jsonschema:"Distribution to build against (e.g., openSUSE_Tumbleweed)."`
	Arch              string `json:"arch,omitempty" jsonschema:"Architecture to build for (e.g., x86_64)."`
}

func (cred *OSCCredentials) Build(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[BuildParam]) (*mcp.CallToolResultFor[any], error) {
	if params.Arguments.ProjectName == "" {
		return nil, fmt.Errorf("project name must be specified")
	}
	if params.Arguments.PackageName == "" {
		return nil, fmt.Errorf("package name must be specified")
	}

	args := []string{"build", "--clean", "--trust-all-projects"}

	if params.Arguments.VmType != "" {
		args = append(args, "--vm-type", params.Arguments.VmType)
	}
	if params.Arguments.MultibuildPackage != "" {
		args = append(args, "-M", params.Arguments.MultibuildPackage)
	}

	dist := params.Arguments.Distribution
	arch := params.Arguments.Arch

	if dist == "" || arch == "" {
		meta, err := cred.getProjectMetaInternal(ctx, params.Arguments.ProjectName)
		if err != nil {
			return nil, fmt.Errorf("failed to get project meta to determine distribution and arch: %w", err)
		}

		if dist == "" {
			if len(meta.Repositories) > 0 {
				dist = meta.Repositories[0].Name
			} else {
				return nil, fmt.Errorf("no distribution specified and could not determine one from project meta")
			}
		}
		if arch == "" {
			if len(meta.Repositories) > 0 && len(meta.Repositories[0].Arches) > 0 {
				hostArch := runtime.GOARCH
				// openSUSE uses x86_64, not amd64
				if hostArch == "amd64" {
					hostArch = "x86_64"
				}
				availableArches := meta.Repositories[0].Arches
				archFound := false
				for _, a := range availableArches {
					if a == hostArch {
						arch = hostArch
						archFound = true
						slog.Info("no architecture specified, using host architecture", slog.String("arch", arch))
						break
					}
				}
				if !archFound {
					arch = availableArches[0]
					slog.Info("no architecture specified, using first available architecture", slog.String("arch", arch))
				}
			} else {
				return nil, fmt.Errorf("no architecture specified and could not determine one from project meta")
			}
		}
	}

	if dist != "" {
		args = append(args, dist)
	}
	if arch != "" {
		args = append(args, arch)
	}

	cmdDir := filepath.Join(cred.TempDir, params.Arguments.ProjectName, params.Arguments.PackageName)
	oscCmd := exec.CommandContext(ctx, "osc", args...)
	oscCmd.Dir = cmdDir

	var out bytes.Buffer
	oscCmd.Stdout = &out
	oscCmd.Stderr = &out

	slog.Info("executing osc build", slog.String("command", oscCmd.String()), slog.String("dir", cmdDir))

	err := oscCmd.Run()

	buildLog := buildlog.Parse(out.String())
	if err != nil {
		slog.Error("failed to parse build log", "error", err)
		// Continue without a parsed log
	}
	buildKey := fmt.Sprintf("%s/%s:%s:%s", params.Arguments.ProjectName, params.Arguments.PackageName, arch, dist)
	if cred.BuildLogs == nil {
		cred.BuildLogs = make(map[string]*buildlog.BuildLog)
	}
	cred.BuildLogs[buildKey] = buildLog
	cred.LastBuildKey = buildKey

	var resultData any
	if err != nil {
		slog.Error("failed to run osc build", slog.String("command", oscCmd.String()), slog.String("output", out.String()))
		resultData = struct {
			Error     string             `json:"error"`
			ParsedLog *buildlog.BuildLog `json:"parsed_log"`
		}{
			Error:     err.Error(),
			ParsedLog: buildLog,
		}
	} else {
		resultData = struct {
			Success       bool           `json:"success"`
			PackagesBuilt []string       `json:"packages_built"`
			RpmLint       map[string]any `json:"lint_report"`
		}{
			Success:       true,
			PackagesBuilt: []string{},       // This needs to be populated
			RpmLint:       map[string]any{}, // This needs to be populated
		}
	}

	jsonBytes, err := json.MarshalIndent(resultData, "", "  ")
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
