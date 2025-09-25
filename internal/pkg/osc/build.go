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
	ProjectName       string   `json:"project_name" jsonschema:"Name of the project"`
	BundleName        string   `json:"bundle_name" jsonschema:"Name of the source package or bundle."`
	VmType            string   `json:"vm_type,omitempty" jsonschema:"VM type to use for build (e.g., chroot, kvm, podman, docker)"`
	MultibuildPackage string   `json:"multibuild_package,omitempty" jsonschema:"Specify the flavor of a multibuild package"`
	Distribution      string   `json:"distribution,omitempty" jsonschema:"Distribution to build against (e.g., openSUSE_Tumbleweed)."`
	Arch              string   `json:"arch,omitempty" jsonschema:"Architecture to build for (e.g., x86_64)."`
	RunService        []string `json:"run_service,omitempty" jsonschema:"A list of services which are run before the build. Useful services are: download_files which downloads the source files reference via an URI in the spec file, go_modules which creates a vendor directory for go files. If empty only build is run. If other services are present, build must be explitly appended."`
}

type BuildResult struct {
	Error         string             `json:"error,omitempty"`
	Success       bool               `json:"success"`
	PackagesBuilt []string           `json:"packages_built,omitempty"`
	RpmLint       map[string]any     `json:"lint_report,omitempty"`
	ParsedLog     *buildlog.BuildLog `json:"parsed_log,omitempty"`
}

func (cred *OSCCredentials) Build(ctx context.Context, req *mcp.CallToolRequest, params BuildParam) (*mcp.CallToolResult, any, error) {
	slog.Debug("mcp tool call: Build", "session", req.Session.ID(), "params", params)
	if params.ProjectName == "" {
		return nil, BuildResult{}, fmt.Errorf("project name must be specified")
	}
	if params.BundleName == "" {
		return nil, BuildResult{}, fmt.Errorf("package or bundle name must be specified")
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

	// Remove "build" if it exists
	filteredServices := []string{}
	for _, s := range params.RunService {
		if s != "build" {
			filteredServices = append(filteredServices, s)
		}
	}
	if len(filteredServices) == 0 {
		filteredServices = append(filteredServices, "build")
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
	var buildErr error
	var result BuildResult
	for _, buildStage := range filteredServices {
		cmdline := cmdlineCfg
		if buildStage == "build" {
			cmdline = append(cmdline, "build", "--clean", "--trust-all-projects")
			if params.VmType != "" {
				cmdline = append(cmdline, "--vm-type", params.VmType, dist, arch)
			}
			if params.MultibuildPackage != "" {
				cmdline = append(cmdline, "-M", params.MultibuildPackage)
			}

		} else {
			cmdline = append(cmdline, "service", "runall", buildStage)
		}
		oscCmd := exec.CommandContext(ctx, cmdline[0], cmdline[1:]...)
		oscCmd.Dir = cmdDir

		stdout, err := oscCmd.StdoutPipe()
		if err != nil {
			return nil, BuildResult{Error: "failed to get stdout pipe: " + err.Error(), Success: false}, nil
		}
		oscCmd.Stderr = oscCmd.Stdout
		slog.Info("starting osc build", slog.String("command", oscCmd.String()), slog.String("dir", cmdDir))
		if err := oscCmd.Start(); err != nil {
			slog.Error("failed to start osc build", "error", err)
			return nil, BuildResult{Error: "failed to start build: " + err.Error(), Success: false}, nil
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

		buildErr = oscCmd.Wait()

		buildLog := buildlog.Parse(out.String())

		if buildStage == "build" {
			buildKey := fmt.Sprintf("%s/%s:%s:%s", params.ProjectName, params.BundleName, arch, dist)
			if cred.BuildLogs == nil {
				cred.BuildLogs = make(map[string]*buildlog.BuildLog)
			}
			cred.BuildLogs[buildKey] = buildLog
			cred.LastBuildKey = buildKey
		}
		if buildErr != nil {
			slog.Error("failed to run service/build", slog.String("command", oscCmd.String()), "error", buildErr)
			result = BuildResult{
				Error:     buildErr.Error(),
				ParsedLog: buildLog,
				Success:   false,
			}
		} else {
			slog.Debug("osc build/service finished successfully", slog.String("command", oscCmd.String()))
			result = BuildResult{
				Success:       true,
				PackagesBuilt: []string{},
				RpmLint:       map[string]any{},
				ParsedLog:     buildLog,
			}
		}
	}

	return nil, result, nil
}
