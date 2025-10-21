package osc

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openSUSE/osc-mcp/internal/pkg/buildlog"
)

var ErrBuildLogNotFound = errors.New("build log not found")

func (cred *OSCCredentials) getFromApi(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "osc-mcp")
	req.SetBasicAuth(cred.Name, cred.Passwd)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	return bodyBytes, resp.StatusCode, nil
}

func (cred *OSCCredentials) getFromApiWithProgress(ctx context.Context, url string, req *mcp.CallToolRequest) ([]byte, int, error) {
	slog.Debug("getFromApiWithProgress", "url", url)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("User-Agent", "osc-mcp")
	httpReq.SetBasicAuth(cred.Name, cred.Passwd)

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	progressToken := req.Params.GetProgressToken()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	progressCtx, cancelProgress := context.WithCancel(context.Background())
	defer cancelProgress()

	go func() {
		for {
			select {
			case <-ticker.C:
				slog.Debug("sending progress notification for build log download")
				err := req.Session.NotifyProgress(progressCtx, &mcp.ProgressNotificationParams{
					ProgressToken: progressToken,
					Message:       "Downloading build log...",
				})
				if err != nil {
					slog.Warn("failed to send progress notification", "error", err)
				}
			case <-progressCtx.Done():
				return
			}
		}
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	return bodyBytes, resp.StatusCode, nil
}

type BuildStatus struct {
	XMLName xml.Name `xml:"status"`
	Code    string   `xml:"code,attr"`
	Details string   `xml:"details"`
}

type MultibuildStatus struct {
	Package string `json:"package,omitempty"`
	Status  string `json:"status,omitempty"`
	Details string `json:"details,omitempty"`
}

// GetBuildStatus retrieves the build status for a given package.
func (cred *OSCCredentials) GetBuildStatus(ctx context.Context, projectName, repositoryName, architectureName, packageName string) (*BuildStatus, error) {
	url := fmt.Sprintf("%s/build/%s/%s/%s/%s/_status", cred.GetAPiAddr(), projectName, repositoryName, architectureName, packageName)
	body, statusCode, err := cred.getFromApi(ctx, url)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get build status: status code %d, body: %s", statusCode, string(body))
	}

	status := BuildStatus{}
	if err := xml.Unmarshal(body, &status); err != nil {
		return nil, fmt.Errorf("failed to parse build status XML: %w", err)
	}
	return &status, nil
}

func (cred *OSCCredentials) getMultibuildStatus(ctx context.Context, projectName, repositoryName, architectureName, packageName string, req *mcp.CallToolRequest) ([]MultibuildStatus, error) {
	slog.Debug("getMultibuildStatus", "project", projectName, "repository", repositoryName, "architecture", architectureName, "package", packageName)

	packages, err := cred.listProjectPackages(ctx, projectName)
	if err != nil {
		return nil, fmt.Errorf("failed to list packages for project %s: %w", projectName, err)
	}

	basePackageName := packageName
	if strings.Contains(packageName, ":") {
		basePackageName = strings.Split(packageName, ":")[0]
	}

	var pkg *Package
	for _, p := range packages {
		if p.Name == basePackageName {
			pkg = p
			break
		}
	}

	if pkg == nil {
		slog.Warn("base package not found for multibuild status", "project", projectName, "package", packageName, "basePackageName", basePackageName)
		return []MultibuildStatus{}, nil
	}

	progressToken := req.Params.GetProgressToken()
	var statuses []MultibuildStatus

	for key := range pkg.Status {
		parts := strings.Split(key, "/")
		if len(parts) < 2 {
			continue
		}
		repo := parts[0]
		arch := parts[1]

		if repo != repositoryName || arch != architectureName {
			continue
		}

		var flavor string
		if len(parts) > 2 {
			flavor = strings.Join(parts[2:], "/")
		}

		var fullPackageName string
		if flavor != "" {
			fullPackageName = fmt.Sprintf("%s:%s", basePackageName, flavor)
		} else {
			fullPackageName = basePackageName
		}

		if progressToken != "" {
			err := req.Session.NotifyProgress(context.Background(), &mcp.ProgressNotificationParams{
				ProgressToken: progressToken,
				Message:       fmt.Sprintf("Checking status of %s...", fullPackageName),
			})
			if err != nil {
				slog.Warn("failed to send progress notification", "error", err)
			}
		}

		status, err := cred.GetBuildStatus(ctx, projectName, repositoryName, architectureName, fullPackageName)
		if err != nil {
			statuses = append(statuses, MultibuildStatus{
				Package: fullPackageName,
				Status:  "error",
				Details: err.Error(),
			})
		} else {
			statuses = append(statuses, MultibuildStatus{
				Package: fullPackageName,
				Status:  status.Code,
				Details: status.Details,
			})
		}
	}

	return statuses, nil
}

type BuildDepInfo struct {
	XMLName  xml.Name          `xml:"builddepinfo"`
	Packages []BuildLogPackage `xml:"package"`
}

type BuildLogPackage struct {
	XMLName xml.Name `xml:"package"`
	Name    string   `xml:"name,attr"`
	Deps    []Dep    `xml:"dep"`
}

type Dep struct {
	XMLName xml.Name `xml:"dep"`
	Name    string   `xml:"name,attr"`
	State   string   `xml:"state,attr"`
}

// GetBuildDepInfo retrieves the build dependency information for a project.
func (cred *OSCCredentials) GetBuildDepInfo(ctx context.Context, projectName, repositoryName, architectureName string) (*BuildDepInfo, error) {
	url := fmt.Sprintf("%s/build/%s/%s/%s/_builddepinfo", cred.GetAPiAddr(), projectName, repositoryName, architectureName)
	slog.Debug("GetBuildDepInfo", "url", url)
	body, statusCode, err := cred.getFromApi(ctx, url)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get build dependency info: status code %d, body: %s", statusCode, string(body))
	}

	var depInfo BuildDepInfo
	if err := xml.Unmarshal(body, &depInfo); err != nil {
		return nil, fmt.Errorf("failed to parse build dependency info XML: %w", err)
	}
	return &depInfo, nil
}

// GetBuildLogRaw retrieves the build log for a given package and returns the raw content.
func (cred *OSCCredentials) GetBuildLogRaw(ctx context.Context, projectName, repositoryName, architectureName, packageName string, req *mcp.CallToolRequest) (string, error) {
	slog.Debug("GetBuildLogRaw", "project", projectName, "repository", repositoryName, "architecture", architectureName, "package", packageName)
	url := fmt.Sprintf("%s/build/%s/%s/%s/%s/_log", cred.GetAPiAddr(), projectName, repositoryName, architectureName, packageName)
	var bodyBytes []byte
	var statusCode int
	var err error

	bodyBytes, statusCode, err = cred.getFromApiWithProgress(ctx, url, req)

	if err != nil {
		return "", err
	}

	if statusCode == http.StatusOK {
		return string(bodyBytes), nil
	}

	if statusCode == http.StatusNotFound {
		return "", ErrBuildLogNotFound
	}

	return "", fmt.Errorf("failed to get build log: status code %d, body: %s", statusCode, string(bodyBytes))
}

const defArch = "x86_64"
const maxLines = 1000

type BuildLogParam struct {
	ProjectName      string `json:"project_name" jsonschema:"Name of the project"`
	PackageName      string `json:"package_name" jsonschema:"Name of the package"`
	Flavor           string `json:"flavor,omitempty" jsonschema:"Flavor of the package. In most cases leave this empty, build falvors only exist if there is a _multibuild file in the source."`
	RepositoryName   string `json:"repository_name" jsonschema:"Repository name, use openSUSE_Tumblweed if the not requested otherwise"`
	ArchitectureName string `json:"architecture_name,omitempty" jsonschema:"Architecture name"`
	NrLines          int    `json:"nr_lines,omitempty" jsonschema:"Maximum number of lines"`
	Offest           int    `json:"offset,omitempty" jsonschema:"Offset from where to starti. If the offset if 0, the last 1000 lines are returned."`
	Exclude          string `json:"exclude,omitempty" jsonschema:"Exclude lines with the given regular expression. Only use this option for logs with more than 1000 lines. Call the tool without this paramater first."`
	Match            string `json:"match,omitempty" jsonschema:"Include only lines matchine this regular expression. Only use this option for logs with more than 1000 lines. Call the tool without this paramater first."`
	ShowSucceeded    bool   `json:"show_succeeded,omitempty" jsonschema:"Also show succeeded logs"`
}

func (cred *OSCCredentials) BuildLog(ctx context.Context, req *mcp.CallToolRequest, params BuildLogParam) (*mcp.CallToolResult, map[string]any, error) {
	slog.Debug("mcp tool call: BuildLog", "params", params)
	if params.ProjectName == "" {
		return nil, nil, fmt.Errorf("project name must be specified")
	}
	if params.PackageName == "" {
		return nil, nil, fmt.Errorf("package name must be specified")
	}
	if params.RepositoryName == "" {
		return nil, nil, fmt.Errorf("repository name must be specified")
	}
	if params.ArchitectureName == "" {
		params.ArchitectureName = defArch
	}

	packageNameWithFlavor := params.PackageName
	if params.Flavor != "" {
		packageNameWithFlavor = fmt.Sprintf("%s:%s", params.PackageName, params.Flavor)
	}

	rawLog, err := cred.GetBuildLogRaw(ctx, params.ProjectName, params.RepositoryName, params.ArchitectureName, packageNameWithFlavor, req)
	if err == nil {
		log := buildlog.Parse(rawLog)
		nrLines := params.NrLines
		if nrLines == 0 || nrLines > maxLines {
			nrLines = maxLines
		}
		result := log.FormatJson(maxLines, params.Offest, params.ShowSucceeded, params.Match, params.Exclude)
		return nil, result, nil
	}

	result := map[string]any{}
	if errors.Is(err, ErrBuildLogNotFound) {
		multibuildStatuses, mbErr := cred.getMultibuildStatus(ctx, params.ProjectName, params.RepositoryName, params.ArchitectureName, params.PackageName, req)
		if mbErr != nil {
			slog.Warn("failed to get multibuild status", "error", mbErr)
		}

		otherFlavors := []MultibuildStatus{}
		for _, status := range multibuildStatuses {
			if status.Package != packageNameWithFlavor {
				otherFlavors = append(otherFlavors, status)
			}
		}

		if len(otherFlavors) > 0 {
			result["message"] = fmt.Sprintf("Build log for package '%s' not found.", packageNameWithFlavor)
			result["other_flavors_status"] = otherFlavors
			return nil, result, nil
		}
		// It's a 404, but there are no other flavors, so let's provide a more detailed error.
		status, statusErr := cred.GetBuildStatus(ctx, params.ProjectName, params.RepositoryName, params.ArchitectureName, packageNameWithFlavor)
		if statusErr != nil {
			return nil, nil, fmt.Errorf("failed to get build log (not found), and also failed to get build status: %w", statusErr)
		}

		if status.Code == "unresolvable" {
			depInfo, depErr := cred.GetBuildDepInfo(ctx, params.ProjectName, params.RepositoryName, params.ArchitectureName)
			if depErr != nil {
				return nil, nil, fmt.Errorf("package is unresolvable, but failed to get dependency info: %w", depErr)
			}

			var missingDeps []string
			for _, p := range depInfo.Packages {
				if p.Name == packageNameWithFlavor {
					for _, d := range p.Deps {
						if d.State == "missing" {
							missingDeps = append(missingDeps, d.Name)
						}
					}
					break
				}
			}

			if len(missingDeps) > 0 {
				return nil, nil, fmt.Errorf("package is unresolvable. Missing dependencies: %s", strings.Join(missingDeps, ", "))
			}

			return nil, nil, fmt.Errorf("package is unresolvable, but could not determine missing dependencies. Details: %s", status.Details)
		} else if status.Code == "excluded" {
			return nil, nil, fmt.Errorf("EXCLUDED '%s': %v", status.Code, multibuildStatuses)
		}

		return nil, nil, fmt.Errorf("failed to get build log: not found, build status is '%s': %s", status.Code, status.Details)
	}

	// It's a different error (e.g., 500, network error)
	result["error"] = err.Error()
	return nil, result, nil
}
