package osc

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/beevik/etree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchSrcBundleParam struct {
	Name     string   `json:"package_name,omitempty" jsonschema:"Name of the source package to search"`
	Projects []string `json:"projects,omitempty" jsonschema:"Optional list of projects to search in"`
}

type BundleInfo struct {
	Name        string `json:"name"`
	Project     string `json:"project"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type BundleOut struct {
	Result []BundleInfo `json:"result" jsonschema:"List of found bundles."`
}

func listLocalPackages(path string, packageName string) ([]BundleInfo, error) {
	var bundles []BundleInfo
	err := filepath.WalkDir(path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasSuffix(path, "/.osc") {
			prjDir := filepath.Dir(path)
			prjName := filepath.Base(prjDir)
			if packageName != "" && prjName != packageName {
				return nil
			}
			bundles = append(bundles, BundleInfo{Project: prjDir, Name: prjName})
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return bundles, nil
}

func (cred OSCCredentials) SearchSrcBundle(ctx context.Context, req *mcp.CallToolRequest, params SearchSrcBundleParam) (*mcp.CallToolResult, any, error) {
	isLocal := false
	if len(params.Projects) == 1 && strings.EqualFold("local", strings.ToLower(params.Projects[0])) || (len(params.Projects) == 0 && params.Name == "") {
		isLocal = true
	}
	if isLocal {
		var bundles []BundleInfo
		bundles, err := listLocalPackages(cred.TempDir, params.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to list local packages in '%s': %w", cred.TempDir, err)
		}
		return nil, BundleOut{Result: bundles}, nil
	}

	var matches []string
	if params.Name != "" {
		matches = append(matches, fmt.Sprintf("@name='%s'", params.Name))
	}
	if len(params.Projects) > 0 {
		var projectMatches []string
		for _, p := range params.Projects {
			projectMatches = append(projectMatches, fmt.Sprintf("@project='%s'", p))
		}
		matches = append(matches, fmt.Sprintf("(%s)", strings.Join(projectMatches, " or ")))
	}
	match := strings.Join(matches, " and ")

	apiURL, err := url.Parse(fmt.Sprintf("https://%s/search/package", cred.Apiaddr))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse API URL: %w", err)
	}
	q := apiURL.Query()
	q.Set("match", match)
	apiURL.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, "GET", apiURL.String(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("User-Agent", "osc-mcp")
	httpReq.SetBasicAuth(cred.Name, cred.Passwd)
	httpReq.Header.Set("Accept", "application/xml; charset=utf-8")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("api request failed with status: %s", resp.Status)
	}

	doc := etree.NewDocument()
	if _, err := doc.ReadFrom(resp.Body); err != nil {
		return nil, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var packages []BundleInfo
	for _, pkg := range doc.FindElements("//package") {
		p := BundleInfo{
			Name:    pkg.SelectAttrValue("name", ""),
			Project: pkg.SelectAttrValue("project", ""),
		}
		if title := pkg.SelectElement("title"); title != nil {
			p.Title = title.Text()
		}
		if description := pkg.SelectElement("description"); description != nil {
			p.Description = description.Text()
		}
		packages = append(packages, p)
	}

	return nil, BundleOut{
		Result: packages,
	}, nil
}

type SearchPackagesParams struct {
	Path            string `json:"path"`
	Path_repository string `json:"path_repository"`
	Arch            string `json:"arch,omitempty"`
	Pattern         string `json:"pattern" jsonschema:"package name to search for, matches any package for which pattern is substring."`
	ExactMatch      bool   `json:"exact,omitempty" jsonschema:"treat pattern as exact match"`
	Regexp          bool   `json:"regexp,omitempty" jsonschema:"treat pattern as regexp"`
}

type SearchPackagesResult struct {
	Packages []rpm_pack `json:"packages"`
}

type rpm_pack struct {
	Name    string
	Arch    string
	Version string
}

// parseRPMFileName extracts the package name from an RPM filename.
// e.g. "pkg-name-1.2.3-1.x86_64.rpm" -> "rpm_pack {Name: pkg-name, Version 1.2.3-1, Arch: x86_64}"
func parseRPMFileName(filename string) rpm_pack {
	if !strings.HasSuffix(filename, ".rpm") {
		return rpm_pack{}
	}
	workstring := filename[:len(filename)-4]

	lastDot := strings.LastIndex(workstring, ".")
	if lastDot == -1 {
		return rpm_pack{}
	}
	arch := workstring[lastDot+1:]
	workstring = workstring[:lastDot] // <name>-<version>-<release>

	releaseDash := strings.LastIndex(workstring, "-")
	if releaseDash == -1 {
		return rpm_pack{Name: workstring, Arch: arch}
	}

	// Heuristic: if the "release" part contains no digits, it's part of the name
	release := workstring[releaseDash+1:]
	if !strings.ContainsAny(release, "0123456789") {
		return rpm_pack{Name: workstring, Arch: arch}
	}

	versionCand := workstring[:releaseDash]
	versionDash := strings.LastIndex(versionCand, "-")

	if versionDash == -1 {
		return rpm_pack{Name: versionCand, Arch: arch, Version: release}
	}

	// Heuristic: if the "version" part does not start with a digit, it's part of the name
	version := versionCand[versionDash+1:]
	if len(version) == 0 || !unicode.IsDigit(rune(version[0])) {
		return rpm_pack{Name: versionCand, Arch: arch, Version: release}
	}

	name := versionCand[:versionDash]
	return rpm_pack{Name: name, Arch: arch, Version: version + "-" + release}
}

func (cred OSCCredentials) SearchPackages(ctx context.Context, req *mcp.CallToolRequest, params SearchPackagesParams) (*mcp.CallToolResult, any, error) {
	if params.ExactMatch && params.Regexp {
		return nil, nil, fmt.Errorf("pattern can't be matched exactly and as a regexp at the same time")
	}
	if !strings.HasPrefix(cred.Apiaddr, "api.") {
		return nil, nil, fmt.Errorf("unexpected api address format: %s", cred.Apiaddr)
	}
	apiaddr := "download." + strings.TrimPrefix(cred.Apiaddr, "api.")

	repoPath := "/repositories/" + strings.ReplaceAll(params.Path, ":", ":/")
	if params.Path_repository != "" {
		repoPath = repoPath + "/" + params.Path_repository
	}

	downloadURL, err := url.Parse(fmt.Sprintf("https://%s%s/INDEX.gz", apiaddr, repoPath))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse download URL: %w", err)
	}

	cacheDir := filepath.Join(cred.TempDir, ".cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	cacheKey := strings.ReplaceAll(downloadURL.Path, "/", "_")
	cacheFile := filepath.Join(cacheDir, cacheKey)

	if _, err := os.Stat(cacheFile); os.IsNotExist(err) {
		httpReq, err := http.NewRequestWithContext(ctx, "GET", downloadURL.String(), nil)
		slog.Debug("downloading", "url", downloadURL.String())
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create request: %w", err)
		}

		client := &http.Client{}
		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to execute request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("download failed with status: %s", resp.Status)
		}

		f, err := os.Create(cacheFile)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create cache file: %w", err)
		}
		if _, err := io.Copy(f, resp.Body); err != nil {
			f.Close()
			return nil, nil, fmt.Errorf("failed to write to cache file: %w", err)
		}
		f.Close()
	}

	f, err := os.Open(cacheFile)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open cache file: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gz.Close()

	var re *regexp.Regexp
	if params.Regexp {
		re, err = regexp.Compile(params.Pattern)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid regexp pattern: %w", err)
		}
	}

	result := SearchPackagesResult{}
	scanner := bufio.NewScanner(gz)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasSuffix(line, ".rpm") {
			continue
		}
		rpmFile := filepath.Base(line)

		actualPackage := parseRPMFileName(rpmFile)
		if actualPackage.Name == "" {
			continue
		}

		match := false
		if params.Pattern == "" {
			match = true
		} else if params.Regexp {
			if re.MatchString(actualPackage.Name) {
				match = true
			}
		} else if params.ExactMatch {
			if actualPackage.Name == params.Pattern {
				match = true
			}
		} else {
			if strings.Contains(actualPackage.Name, params.Pattern) {
				match = true
			}
		}

		if match {
			result.Packages = append(result.Packages, actualPackage)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("error reading gzipped index: %w", err)
	}
	return nil, result, nil
}
