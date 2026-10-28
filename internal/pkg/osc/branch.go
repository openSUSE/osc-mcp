package osc

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type BranchPackageParam struct {
	Project       string `json:"project_name" jsonschema:"The project from which the package is branched."`
	Bundle        string `json:"bundle_name" jsonschema:"The bundle or source package that you want to branch or copy."`
	TargetProject string `json:"target_project,omitempty" jsonschema:"The target project to branch the package to. If not provided, a new project will be created."`
	Copy          bool   `json:"copy,omitempty" jsonschema:"Copy the package instead of branching."`
}

// existingBranchRegex matches the summary OBS returns when the branch target exists, as osc does
var existingBranchRegex = regexp.MustCompile(`^branch target package already exists(?:: (\S+)/(\S+))?`)

// obsStatus is the error document OBS sends with a failed request
type obsStatus struct {
	XMLName xml.Name `xml:"status"`
	Code    string   `xml:"code,attr"`
	Summary string   `xml:"summary"`
}

// safeName reports whether a project or package name from OBS can be used as a single path element
func safeName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
}

type BranchResult struct {
	TargetProject string `json:"target_project"`
	TargetPackage string `json:"target_package"`
	CheckoutDir   string `json:"checkout_dir"`
	Existing      bool   `json:"existing"`
}

func (cred OSCCredentials) BranchBundle(ctx context.Context, req *mcp.CallToolRequest, params BranchPackageParam) (*mcp.CallToolResult, BranchResult, error) {
	slog.Debug("mcp tool call: BranchBundle", "session", req.Session.ID(), "params", params)
	if params.Project == "" {
		return nil, BranchResult{}, fmt.Errorf("project name cannot be empty")
	}
	if params.Bundle == "" {
		return nil, BranchResult{}, fmt.Errorf("package name cannot be empty")
	}

	targetProject := params.TargetProject
	if targetProject == "" {
		targetProject = fmt.Sprintf("home:%s:branches:%s", cred.Name, params.Project)
	}
	targetPackage := params.Bundle

	apiURL, err := url.Parse(fmt.Sprintf("%s/source/%s/%s", cred.GetAPiAddr(), params.Project, params.Bundle))
	if err != nil {
		return nil, BranchResult{}, fmt.Errorf("failed to parse API URL: %w", err)
	}
	q := apiURL.Query()
	if params.Copy {
		q.Set("cmd", "copy")
	} else {
		q.Set("cmd", "branch")
	}
	q.Set("target_project", targetProject)
	q.Set("target_package", targetPackage)
	apiURL.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL.String(), nil)
	if err != nil {
		return nil, BranchResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("User-Agent", "osc-mcp")
	httpReq.SetBasicAuth(cred.Name, cred.Passwd)

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, BranchResult{}, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	existing := false
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		if err != nil {
			return nil, BranchResult{}, fmt.Errorf("api request failed with status: %s, failed to read response body: %w", resp.Status, err)
		}
		var status obsStatus
		var match []string
		if resp.StatusCode == http.StatusBadRequest && xml.Unmarshal(body, &status) == nil {
			match = existingBranchRegex.FindStringSubmatch(status.Summary)
			// the code decides; an answer without a code is judged by the summary alone, as osc does
			existing = status.Code == "double_branch_package" || (status.Code == "" && match != nil)
		}
		if !existing {
			return nil, BranchResult{}, fmt.Errorf("api request failed with status: %s\n%s", resp.Status, string(body))
		}
		// check out the branch where OBS says it is; keep the requested pair if the summary names none
		if match != nil && match[1] != "" {
			if !safeName(match[1]) || !safeName(match[2]) {
				return nil, BranchResult{}, fmt.Errorf("unexpected branch location in OBS answer: %s", status.Summary)
			}
			targetProject, targetPackage = match[1], match[2]
		}
		slog.Info("branch already exists, checking it out", "project", targetProject, "package", targetPackage)
	}

	checkoutDir := filepath.Join(cred.TempDir, targetProject, targetPackage)
	if _, err := os.Stat(checkoutDir); err == nil { // directory exists
		cmd := exec.CommandContext(ctx, "osc", "update")
		cmd.Dir = checkoutDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, BranchResult{}, fmt.Errorf("failed to run '%s' in '%s': %w\n%s", cmd.String(), checkoutDir, err, string(output))
		}
	} else if os.IsNotExist(err) { // directory does not exist
		cmd := exec.CommandContext(ctx, "osc", "checkout", targetProject, targetPackage)
		cmd.Dir = cred.TempDir
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, BranchResult{}, fmt.Errorf("failed to run '%s': %w\n%s", cmd.String(), err, string(output))
		}
	} else { // some other error
		return nil, BranchResult{}, fmt.Errorf("failed to check directory '%s': %w", checkoutDir, err)
	}

	result := BranchResult{
		TargetProject: targetProject,
		TargetPackage: targetPackage,
		CheckoutDir:   checkoutDir,
		Existing:      existing,
	}

	text := fmt.Sprintf("branch created at %s/%s", targetProject, targetPackage)
	if params.Copy {
		text = fmt.Sprintf("package copied to %s/%s", targetProject, targetPackage)
	}
	if existing {
		text = fmt.Sprintf("existing branch checked out at %s/%s (it may be behind %s/%s; `osc pull` or re-branch to sync)", targetProject, targetPackage, params.Project, params.Bundle)
	}
	// the JSON block stays first, as the SDK sends it when the handler sets no content
	out, err := json.Marshal(result)
	if err != nil {
		return nil, BranchResult{}, fmt.Errorf("failed to marshal result: %w", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(out)}, &mcp.TextContent{Text: text}}}, result, nil
}
