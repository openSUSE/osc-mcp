package osc

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
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

type BuildResult struct {
	Error         string             `json:"error,omitempty"`
	Success       bool               `json:"success"`
	PackagesBuilt []string           `json:"packages_built,omitempty"`
	RpmLint       map[string]any     `json:"lint_report,omitempty"`
	ParsedLog     *buildlog.BuildLog `json:"parsed_log,omitempty"`
}

func (cred *OSCCredentials) Build(ctx context.Context, req *mcp.CallToolRequest, params BuildParam) (*mcp.CallToolResult, any, error) {
	if params.ProjectName == "" {
		return nil, BuildResult{}, fmt.Errorf("project name must be specified")
	}
	if params.PackageName == "" {
		return nil, BuildResult{}, fmt.Errorf("package name must be specified")
	}

	cmdline := []string{"osc"}
	configFile, err := cred.writeTempOscConfig()
	if err != nil {
		slog.Warn("failed to write osc config", "error", err)
	} else {
		defer os.Remove(configFile)
		cmdline = append(cmdline, "--config", configFile)
	}

	cmdline = append(cmdline, "build", "--clean", "--trust-all-projects")

	if params.VmType != "" {
		cmdline = append(cmdline, "--vm-type", params.VmType)
	}
	if params.MultibuildPackage != "" {
		cmdline = append(cmdline, "-M", params.MultibuildPackage)
	}

	dist := params.Distribution
	arch := params.Arch

	if dist == "" || arch == "" {
		meta, err := cred.getProjectMetaInternal(ctx, params.ProjectName)
		if err != nil {
			return nil, BuildResult{}, fmt.Errorf("failed to get project meta to determine distribution and arch: %w", err)
		}

		if dist == "" {
			if len(meta.Repositories) > 0 {
				dist = meta.Repositories[0].Name
			} else {
				return nil, BuildResult{}, fmt.Errorf("no distribution specified and could not determine one from project meta")
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
				return nil, BuildResult{}, fmt.Errorf("no architecture specified and could not determine one from project meta")
			}
		}
	}

	if dist != "" {
		cmdline = append(cmdline, dist)
	}
	if arch != "" {
		cmdline = append(cmdline, arch)
	}

	cmdDir := filepath.Join(cred.TempDir, params.ProjectName, params.PackageName)
	oscCmd := exec.CommandContext(ctx, cmdline[0], cmdline[1:]...)
	oscCmd.Dir = cmdDir

	var out bytes.Buffer
	oscCmd.Stdout = &out
	oscCmd.Stderr = &out

	slog.Info("executing osc build", slog.String("command", oscCmd.String()), slog.String("dir", cmdDir))

	err = oscCmd.Run()

	buildLog := buildlog.Parse(out.String())
	if err != nil {
		slog.Error("failed to parse build log", "error", err)
		// Continue without a parsed log
	}
	buildKey := fmt.Sprintf("%s/%s:%s:%s", params.ProjectName, params.PackageName, arch, dist)
	if cred.BuildLogs == nil {
		cred.BuildLogs = make(map[string]*buildlog.BuildLog)
	}
	cred.BuildLogs[buildKey] = buildLog
	cred.LastBuildKey = buildKey

	if err != nil {
		slog.Error("failed to run osc build", slog.String("command", oscCmd.String()), slog.String("output", out.String()))
		return nil, BuildResult{
			Error:     err.Error(),
			ParsedLog: buildLog,
			Success:   false,
		}, nil
	}

	return nil, BuildResult{
		Success:       true,
		PackagesBuilt: []string{},       // This needs to be populated
		RpmLint:       map[string]any{}, // This needs to be populated
	}, nil
}
