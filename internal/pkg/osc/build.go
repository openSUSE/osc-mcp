package osc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

	buildLog := parseBuildLog(out.String())
	buildKey := fmt.Sprintf("%s/%s:%s:%s", params.Arguments.ProjectName, params.Arguments.PackageName, arch, dist)
	if cred.BuildLogs == nil {
		cred.BuildLogs = make(map[string]*BuildLog)
	}
	cred.BuildLogs[buildKey] = buildLog
	cred.LastBuildKey = buildKey

	var resultData any
	if err != nil {
		slog.Error("failed to run osc build", slog.String("command", oscCmd.String()), slog.String("output", out.String()))
		resultData = struct {
			Error     string    `json:"error"`
			ParsedLog *BuildLog `json:"parsed_log"`
		}{
			Error:     err.Error(),
			ParsedLog: buildLog,
		}
	} else {
		type BuildPhaseResult struct {
			Phase      string `json:"phase"`
			Success    bool   `json:"success"`
			Duration   int    `json:"duration"`
			LinesCount int    `json:"lines_count"`
		}
		resultData = []BuildPhaseResult{
			{"preinstall", buildLog.Preinstall.Success, buildLog.Preinstall.Duration, len(buildLog.Preinstall.Lines)},
			{"copying_packages", buildLog.CopyingPackages.Success, buildLog.CopyingPackages.Duration, len(buildLog.CopyingPackages.Lines)},
			{"vm_boot", buildLog.VMBoot.Success, buildLog.VMBoot.Duration, len(buildLog.VMBoot.Lines)},
			{"package_cumulation", buildLog.PackageCumulation.Success, buildLog.PackageCumulation.Duration, len(buildLog.PackageCumulation.Lines)},
			{"package_installation", buildLog.PackageInstallation.Success, buildLog.PackageInstallation.Duration, len(buildLog.PackageInstallation.Lines)},
			{"build", buildLog.Build.Success, buildLog.Build.Duration, len(buildLog.Build.Lines)},
			{"post_build_checks", buildLog.PostBuildChecks.Success, buildLog.PostBuildChecks.Duration, len(buildLog.PostBuildChecks.Lines)},
			{"rpmlint_report", buildLog.RPMLintReport.Success, buildLog.RPMLintReport.Duration, len(buildLog.RPMLintReport.Lines)},
			{"package_comparison", buildLog.PackageComparison.Success, buildLog.PackageComparison.Duration, len(buildLog.PackageComparison.Lines)},
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



func toBuildPhase(content string) BuildPhase {
	content = strings.TrimSpace(content)
	var lines []string
	if content != "" {
		lines = strings.Split(content, "\n")
	} else {
		lines = []string{}
	}

	if len(lines) == 0 {
		return BuildPhase{Success: true}
	}

	durationRegex := regexp.MustCompile(`^[\[\s*(\d+)s\]]`)
	var firstTimestamp, lastTimestamp int = -1, -1

	for _, line := range lines {
		if matches := durationRegex.FindStringSubmatch(line); len(matches) > 1 {
			ts, err := strconv.Atoi(matches[1])
			if err == nil {
				if firstTimestamp == -1 {
					firstTimestamp = ts
				}
				lastTimestamp = ts
			}
		}
	}

	var duration int
	if firstTimestamp != -1 && lastTimestamp != -1 {
		duration = lastTimestamp - firstTimestamp
	}

	// Simple success check, might need refinement
	success := !strings.Contains(strings.ToLower(content), "error:") && !strings.Contains(strings.ToLower(content), "failed:")

	return BuildPhase{
		Lines:    lines,
		Success:  success,
		Duration: duration,
	}
}

func toSystemInstallation(content string) SystemInstallation {
	buildPhase := toBuildPhase(content)
	var packages []string
	// Example: [    2s] [1/173] keeping compat-usrmerge-tools-84.87-5.22
	packageRegex := regexp.MustCompile(`[\[\s*(\d+)s\]]\s*\[\d+/\d+\]\s+keeping\s+(.+)`) // Corrected regex to match the actual log format

	for _, line := range buildPhase.Lines {
		if matches := packageRegex.FindStringSubmatch(line); len(matches) > 1 {
			packages = append(packages, strings.TrimSpace(matches[1]))
		}
	}

	return SystemInstallation{
		BuildPhase: buildPhase,
		Packages:   packages,
	}
}

// parseBuildLog parses a build log string and splits it into sections.
func parseBuildLog(log string) *BuildLog {
	log = strings.ReplaceAll(log, "\r\n", "\n")
	lines := strings.Split(log, "\n")

	var builders = make(map[string]*strings.Builder)
	for _, name := range []string{"header", "preinstall", "copying_packages", "vm_boot", "package_cumulation", "package_installation", "build", "post_build_checks", "rpmlint_report", "package_comparison", "summary", "retries"} {
		builders[name] = &strings.Builder{}
	}

	current := "header"

	for _, line := range lines {
		// This state machine is adapted for chroot builds based on the provided log.
		// It may need adjustments for other build types (e.g., KVM).
		if strings.Contains(line, "init_buildsystem") && current == "header" {
			current = "preinstall"
		} else if strings.Contains(line, "querying package ids...") && current == "preinstall" {
			current = "package_installation" // This section contains the "keeping" lines
		} else if strings.Contains(line, "Running build time source services...") && (current == "package_installation" || current == "build") {
			current = "build"
		} else if strings.Contains(line, "... checking for files with abuild user/group") && current == "build" {
			current = "post_build_checks"
		} else if strings.Contains(line, "RPMLINT report:") && current == "post_build_checks" {
			current = "rpmlint_report"
		} else if strings.Contains(line, "finished \"build") && (current == "rpmlint_report" || current == "package_comparison" || current == "summary") {
			current = "summary"
		} else if strings.HasPrefix(line, "Retried build at") {
			current = "retries"
		}

		builders[current].WriteString(line + "\n")
	}

	return &BuildLog{
		Header:              strings.TrimSpace(builders["header"].String()),
		Preinstall:          toBuildPhase(builders["preinstall"].String()),
		CopyingPackages:     toBuildPhase(builders["copying_packages"].String()),
		VMBoot:              toBuildPhase(builders["vm_boot"].String()),
		PackageCumulation:   toBuildPhase(builders["package_cumulation"].String()),
		PackageInstallation: toSystemInstallation(builders["package_installation"].String()),
		Build:               toBuildPhase(builders["build"].String()),
		PostBuildChecks:     toBuildPhase(builders["post_build_checks"].String()),
		RPMLintReport:       toBuildPhase(builders["rpmlint_report"].String()),
		PackageComparison:   toBuildPhase(builders["package_comparison"].String()),
		Summary:             strings.TrimSpace(builders["summary"].String()),
		Retries:             strings.TrimSpace(builders["retries"].String()),
	}
}
