package osc

import (
	"bufio"
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
	BundleName        string `json:"bundle_name" jsonschema:"Name of the source package or bundle."`
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
	Buildroot     string             `json:"build-root,omitempty" jsonschema:"The root directory for the build"`
}

type RunServicesParam struct {
	ProjectName string   `json:"project_name" jsonschema:"Name of the project"`
	BundleName  string   `json:"bundle_name" jsonschema:"Name of the source package or bundle."`
	Services    []string `json:"services" jsonschema:"List of services to run. Useful services are: download_files: downloads the source files reference via an URI in the spec file with the pattern https://github.com/foo/baar/v%{version}.tar.gz#./%{name}-%{version}.tar.gz, go_modules: which creates a vendor directory for go files if the source has the same name as the project."`
}

type RunServicesResult struct {
	Error   string `json:"error,omitempty"`
	Success bool   `json:"success"`
	Log     string `json:"log,omitempty"`
}

func (cred *OSCCredentials) RunServices(ctx context.Context, req *mcp.CallToolRequest, params RunServicesParam) (*mcp.CallToolResult, any, error) {
	slog.Debug("mcp tool call: RunServices", "session", req.Session.ID(), "params", params)
	if params.ProjectName == "" {
		return nil, RunServicesResult{Success: false}, fmt.Errorf("project name must be specified")
	}
	if params.BundleName == "" {
		return nil, RunServicesResult{Success: false}, fmt.Errorf("package or bundle name must be specified")
	}
	if len(params.Services) == 0 {
		return nil, RunServicesResult{Success: false}, fmt.Errorf("at least one service must be specified")
	}

	cmdlineCfg := []string{"osc"}
	configFile, err := cred.writeTempOscConfig()
	if err != nil {
		slog.Warn("failed to write osc config", "error", err)
	} else {
		defer os.Remove(configFile)
		cmdlineCfg = append(cmdlineCfg, "--config", configFile)
	}

	cmdDir := filepath.Join(cred.TempDir, params.ProjectName, params.BundleName)
	progressToken := req.Params.GetProgressToken()

	var outAll bytes.Buffer
	for _, service := range params.Services {
		cmdline := append(cmdlineCfg, "service", "runall", service)
		oscCmd := exec.CommandContext(ctx, cmdline[0], cmdline[1:]...)
		oscCmd.Dir = cmdDir

		stdout, err := oscCmd.StdoutPipe()
		if err != nil {
			return nil, RunServicesResult{Error: "failed to get stdout pipe: " + err.Error(), Success: false}, nil
		}
		oscCmd.Stderr = oscCmd.Stdout
		slog.Info("starting osc service run", slog.String("command", oscCmd.String()), slog.String("dir", cmdDir))
		if err := oscCmd.Start(); err != nil {
			slog.Error("failed to start osc service run", "error", err)
			return nil, RunServicesResult{Error: "failed to start service run: " + err.Error(), Success: false}, nil
		}
		var out bytes.Buffer
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			out.WriteString(line)
			out.WriteString("\n")
			if progressToken != nil {
				err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
					ProgressToken: progressToken,
					Message:       line,
				})
				if err != nil {
					slog.Warn("failed to send progress notification", "error", err)
				}
			}
		}
		outAll.Write(out.Bytes())

		err = oscCmd.Wait()
		if err != nil {
			slog.Error("failed to run service", slog.String("command", oscCmd.String()), "error", err)
			return nil, RunServicesResult{
				Error:   err.Error(),
				Success: false,
				Log:     outAll.String(),
			}, nil
		}
		slog.Debug("osc service finished successfully", slog.String("command", oscCmd.String()))
	}

	return nil, RunServicesResult{
		Success: true,
		Log:     outAll.String(),
	}, nil
}

func (cred *OSCCredentials) Build(ctx context.Context, req *mcp.CallToolRequest, params BuildParam) (*mcp.CallToolResult, any, error) {
	result := BuildResult{}
	slog.Debug("mcp tool call: Build", "session", req.Session.ID(), "params", params)
	if params.ProjectName == "" {
		return nil, result, fmt.Errorf("project name must be specified")
	}
	if params.BundleName == "" {
		return nil, result, fmt.Errorf("package or bundle name must be specified")
	}

	cmdline := []string{"osc"}
	configFile, err := cred.writeTempOscConfig()
	if err != nil {
		slog.Warn("failed to write osc config", "error", err)
	} else {
		defer os.Remove(configFile)
		cmdline = append(cmdline, "--config", configFile)
	}

	cmdDir := filepath.Join(cred.TempDir, params.ProjectName, params.BundleName)
	progressToken := req.Params.GetProgressToken()

	dist := params.Distribution
	arch := params.Arch
	if dist == "" || arch == "" {
		meta, err := cred.getProjectMetaInternal(ctx, params.ProjectName)
		if err != nil {
			return nil, result, fmt.Errorf("failed to get project meta to determine distribution and arch: %w", err)
		}

		if dist == "" {
			if len(meta.Repositories) > 0 {
				dist = meta.Repositories[0].Name
			} else {
				return nil, result, fmt.Errorf("no distribution specified and could not determine one from project meta")
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
					slog.Warn("no architecture specified, using first available architecture", slog.String("arch", arch))
				}
			} else {
				return nil, result, fmt.Errorf("no architecture specified and could not determine one from project meta")
			}
		}
	}

	cmdline = append(cmdline, "build", "--clean", "--trust-all-projects")
	if params.VmType != "" && params.VmType != "chroot" {
		cmdline = append(cmdline, "--vm-type", params.VmType, dist, arch)
	} else {
		if cred.BuildRootInWorkdir {
			buildRoot := fmt.Sprintf("%s/build-root/%s-%s", cred.TempDir, dist, arch)
			cmdline = append(cmdline, "--root", buildRoot)
			result.Buildroot = buildRoot
		}
	}
	if params.MultibuildPackage != "" {
		cmdline = append(cmdline, "-M", params.MultibuildPackage)
	}

	oscCmd := exec.CommandContext(ctx, cmdline[0], cmdline[1:]...)
	oscCmd.Dir = cmdDir

	stdout, err := oscCmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	oscCmd.Stderr = oscCmd.Stdout
	slog.Info("starting osc build", slog.String("command", oscCmd.String()), slog.String("dir", cmdDir))
	if err := oscCmd.Start(); err != nil {
		slog.Error("failed to start osc build", "error", err)
		return nil, nil, err
	}
	var out bytes.Buffer
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		out.WriteString(line)
		out.WriteString("\n")
		if progressToken != nil {
			err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
				ProgressToken: progressToken,
				Message:       line,
			})
			if err != nil {
				slog.Warn("failed to send progress notification", "error", err)
			}
		}
	}

	buildErr := oscCmd.Wait()

	buildLog := buildlog.Parse(out.String())

	buildKey := fmt.Sprintf("%s/%s:%s:%s", params.ProjectName, params.BundleName, arch, dist)
	if cred.BuildLogs == nil {
		cred.BuildLogs = make(map[string]*buildlog.BuildLog)
	}
	cred.BuildLogs[buildKey] = buildLog
	cred.LastBuildKey = buildKey

	if buildErr != nil {
		slog.Error("failed to run build", slog.String("command", oscCmd.String()), "error", buildErr)
		result.Error = buildErr.Error()
		result.ParsedLog = buildLog
		result.Success = false
		return nil, result, nil
	}

	slog.Debug("osc build finished successfully", slog.String("command", oscCmd.String()))
	result.Success = true
	result.PackagesBuilt = []string{}
	result.RpmLint = map[string]any{}
	result.ParsedLog = buildLog
	return nil, result, nil
}
