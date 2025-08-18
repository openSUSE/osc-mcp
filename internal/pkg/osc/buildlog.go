package osc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func toBuildPhase(content string) BuildPhase {
	content = strings.TrimSpace(content)
	var lines []string
	if content != "" {
		lines = strings.Split(content, "\n")
	} else {
		lines = []string{}
	}

	if len(lines) == 0 {
		return BuildPhase{Success: false}
	}

	durationRegex := regexp.MustCompile(`^\[\s*(\d+)s\]`)
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

	structured_log := []BuildPhaseResult{}
	current := "header"

	for _, line := range lines {
		strBuild := strings.Builder{}
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

		strBuild.WriteString(line + "\n")
	}

}

// GetBuildLog retrieves the build log for a given package and parses it into a structured format.
func (cred *OSCCredentials) GetBuildLog(ctx context.Context, projectName, repositoryName, architectureName, packageName string) ([]BuildPhaseResult, error) {
	url := fmt.Sprintf("https://%s/build/%s/%s/%s/%s/_log", cred.Apiaddr, projectName, repositoryName, architectureName, packageName)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)

	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(cred.Name, cred.Passwd)

	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to get build log, status code %d, and failed to read body: %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("failed to get build log: status code %d, body: %s", resp.StatusCode, string(bodyBytes))
	}

	bodyBytes, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, fmt.Errorf("failed to read build log response body: %w", err)
	}

	logContent := string(bodyBytes)
	buildLog := parseBuildLog(logContent)

	results := []BuildPhaseResult{
		{Name: "Header", Result: buildLog.Header},
		{Name: "Preinstall", Result: buildLog.Preinstall},
		{Name: "CopyingPackages", Result: buildLog.CopyingPackages},
		{Name: "VMBoot", Result: buildLog.VMBoot},
		{Name: "PackageCumulation", Result: buildLog.PackageCumulation},
		{Name: "PackageInstallation", Result: buildLog.PackageInstallation},
		{Name: "Build", Result: buildLog.Build},
		{Name: "PostBuildChecks", Result: buildLog.PostBuildChecks},
		{Name: "RPMLintReport", Result: buildLog.RPMLintReport},
		{Name: "PackageComparison", Result: buildLog.PackageComparison},
		{Name: "Summary", Result: buildLog.Summary},
		{Name: "Retries", Result: buildLog.Retries},
	}
	var finalResults []BuildPhaseResult
	for _, result := range results {
		var lines []string
		switch v := result.Result.(type) {
		case BuildPhase:
			lines = v.Lines
		case SystemInstallation:
			lines = v.Lines
		}

		if len(lines) > 0 {
			finalResults = append(finalResults, result)
		}
	}

	return finalResults, nil
}

type BuildLogParam struct {
	ProjectName      string `json:"project_name" jsonschema:"Name of the project"`
	PackageName      string `json:"package_name" jsonschema:"Name of the package"`
	RepositoryName   string `json:"repository_name,omitempty" jsonschema:"Repository name"`
	ArchitectureName string `json:"architecture_namei,omitempty" jsonschema:"Architecture name"`
}

const defRepo = "openSUSE_Tumbleweed"
const defArch = "x86_64"

func GetBuildLogSchema() (*jsonschema.Schema, error) {
	schema, err := jsonschema.For[BuildLogParam]()
	if err != nil {
		return nil, err
	}
	schema.AdditionalProperties = &jsonschema.Schema{}
	schema.Default = []byte(`{"repository_name":"` + defRepo + `","architecture_name":"` + defArch + `","project_name":"","package_name":""}`)
	return schema, nil
}

func (cred *OSCCredentials) BuildLog(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[BuildLogParam]) (*mcp.CallToolResultFor[any], error) {
	if params.Arguments.ProjectName == "" {
		return nil, fmt.Errorf("project name must be specified")
	}
	if params.Arguments.PackageName == "" {
		return nil, fmt.Errorf("package name must be specified")
	}
	if params.Arguments.RepositoryName == "" {
		//\FIXME Defaults from jsonschema doesn't seem to work
		params.Arguments.RepositoryName = defRepo
	}
	if params.Arguments.ArchitectureName == "" {
		//\FIXME Defaults from jsonschema doesn't seem to work
		params.Arguments.ArchitectureName = defArch
	}

	results, err := cred.GetBuildLog(ctx, params.Arguments.ProjectName, params.Arguments.RepositoryName, params.Arguments.ArchitectureName, params.Arguments.PackageName)
	if err != nil {
		return nil, err
	}

	jsonBytes, err := json.MarshalIndent(results, "", "  ")
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
