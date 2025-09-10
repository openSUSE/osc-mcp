package osc

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"

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

var (
	currentBuild *buildJob
	buildMu      sync.Mutex
)

type buildJob struct {
	params BuildParam
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan struct{}
	result *BuildResult
}

func (cred *OSCCredentials) Build(ctx context.Context, req *mcp.CallToolRequest, params BuildParam) (*mcp.CallToolResult, any, error) {
	if params.ProjectName == "" {
		return nil, BuildResult{}, fmt.Errorf("project name must be specified")
	}
	if params.PackageName == "" {
		return nil, BuildResult{}, fmt.Errorf("package name must be specified")
	}

	buildMu.Lock()

	if currentBuild != nil {
		if reflect.DeepEqual(currentBuild.params, params) {
			// Same params. Check status.
			select {
			case <-currentBuild.done:
				// Done. Get result and clear.
				result := currentBuild.result
				currentBuild = nil
				buildMu.Unlock()
				return nil, *result, nil
			default:
				// Still running.
				buildMu.Unlock()
				return nil, BuildResult{Error: "A build with the same parameters is already in progress."}, nil
			}
		} else {
			// Different params. Cancel and clear.
			slog.Info("Cancelling previous build due to new request with different parameters.")
			currentBuild.cancel()
			currentBuild = nil
		}
	}

	cmdline := []string{"osc"}
	configFile, err := cred.writeTempOscConfig()
	if err != nil {
		slog.Warn("failed to write osc config", "error", err)
	} else {
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
			buildMu.Unlock()
			return nil, BuildResult{}, fmt.Errorf("failed to get project meta to determine distribution and arch: %w", err)
		}

		if dist == "" {
			if len(meta.Repositories) > 0 {
				dist = meta.Repositories[0].Name
			} else {
				buildMu.Unlock()
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
				buildMu.Unlock()
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

	buildCtx, cancel := context.WithCancel(context.Background())
	cmdDir := filepath.Join(cred.TempDir, params.ProjectName, params.PackageName)
	oscCmd := exec.CommandContext(buildCtx, cmdline[0], cmdline[1:]...)
	oscCmd.Dir = cmdDir

	var out bytes.Buffer
	oscCmd.Stdout = &out
	oscCmd.Stderr = &out

	job := &buildJob{
		params: params,
		cmd:    oscCmd,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	currentBuild = job

	slog.Info("starting osc build in background", slog.String("command", oscCmd.String()), slog.String("dir", cmdDir))

	if err := oscCmd.Start(); err != nil {
		slog.Error("failed to start osc build", "error", err)
		currentBuild = nil
		buildMu.Unlock()
		return nil, BuildResult{Error: "failed to start build: " + err.Error(), Success: false}, nil
	}

	buildMu.Unlock()

	go func() {
		defer os.Remove(configFile)
		buildErr := job.cmd.Wait()

		buildLog := buildlog.Parse(out.String())

		buildKey := fmt.Sprintf("%s/%s:%s:%s", params.ProjectName, params.PackageName, arch, dist)
		if cred.BuildLogs == nil {
			cred.BuildLogs = make(map[string]*buildlog.BuildLog)
		}
		cred.BuildLogs[buildKey] = buildLog
		cred.LastBuildKey = buildKey

		var result BuildResult
		if buildErr != nil {
			slog.Error("failed to run osc build", slog.String("command", job.cmd.String()), slog.String("output", out.String()), "error", buildErr)
			result = BuildResult{
				Error:     buildErr.Error(),
				ParsedLog: buildLog,
				Success:   false,
			}
		} else {
			slog.Info("osc build finished successfully", slog.String("command", job.cmd.String()))
			result = BuildResult{
				Success:       true,
				PackagesBuilt: []string{},
				RpmLint:       map[string]any{},
				ParsedLog:     buildLog,
			}
		}

		buildMu.Lock()
		defer buildMu.Unlock()
		if currentBuild == job {
			job.result = &result
		}

		close(job.done)
	}()

	return nil, BuildResult{Success: true, Error: "Build started in background."}, nil
}
