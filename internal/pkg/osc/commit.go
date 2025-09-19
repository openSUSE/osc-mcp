package osc

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/hbollon/go-edlib"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CommitCmd struct {
	Message             string `json:"message" jsonschema:"Commit message"`
	Directory           string `json:"directory" jsonschema:"Directory of the package to commit"`
	ProjectName         string `json:"project_name,omitempty" jsonschema:"Project name. If not provided, it will be derived from the directory path."`
	BundleName          string `json:"bundle_name,omitemtpy" jsonschema:"Bundle name also known as source package name. If not provided, it will be derived from the directory path."`
	SkipChangesCreation bool   `json:"skip_changes,omitempyt" jsonschema:"Skip the automatic update of the changes file."`
}

type CommitResult struct {
	Revision string `json:"revision"`
}

type Revision struct {
	XMLName xml.Name `xml:"revision"`
	Rev     string   `xml:"rev,attr"`
}

type Directory struct {
	XMLName xml.Name `xml:"directory"`
	Name    string   `xml:"name,attr"`
	Project string   `xml:"project,attr"`
	Entries []Entry  `xml:"entry"`
}

type Entry struct {
	XMLName xml.Name `xml:"entry"`
	Name    string   `xml:"name,attr"`
	Md5     string   `xml:"md5,attr"`
	Size    string   `xml:"size,attr"`
	Mtime   string   `xml:"mtime,attr"`
	Rev     string   `xml:"rev,attr"`
}

func (cred *OSCCredentials) Commit(ctx context.Context, req *mcp.CallToolRequest, params CommitCmd) (*mcp.CallToolResult, CommitResult, error) {
	slog.Debug("mcp tool call: Commit", "session", req.Session.ID(), "params", params)
	if params.Message == "" {
		return nil, CommitResult{}, fmt.Errorf("commit message must be specified")
	}
	if params.Directory == "" {
		return nil, CommitResult{}, fmt.Errorf("directory must be specified")
	}

	projectName := params.ProjectName
	bundleName := params.BundleName
	if projectName == "" {
		projectName = filepath.Base(filepath.Dir(params.Directory))
	}
	if bundleName == "" {
		bundleName = filepath.Base(params.Directory)
	}
	if projectName == "" || bundleName == "" {
		return nil, CommitResult{}, fmt.Errorf("could not determine project and package name from directory: %s", params.Directory)
	}
	if !params.SkipChangesCreation {
		var changesFile string
		if changesFiles, _ := filepath.Glob(path.Join(params.Directory, "*changes")); len(changesFiles) > 0 {
			// only create a changes file if we find a spec file, ergo it's a rpm
			// do some funky math to find the best matching changes file of pkg
			if len(changesFiles) > 1 {
				changesFile, _ = edlib.FuzzySearch(bundleName, changesFiles, edlib.Levenshtein)
			} else {
				changesFile = changesFiles[0]
			}
			// no changes file, let's create one based on a spec files
			if changesFile == "" {
				if specFiles, _ := filepath.Glob(path.Join(params.Directory, "*spec")); len(specFiles) > 0 {
					if len(specFiles) > 1 {
						changesFile, _ = edlib.FuzzySearch(bundleName, specFiles, edlib.Levenshtein)
					} else {
						changesFile = specFiles[0]
					}
					changesFile = strings.TrimSuffix(changesFile, ".spec") + ".changes"
				}
			}
		}
		if changesFile != "" {

			changesEntry := createChangesEntry(params.Message, cred.Name+"-mcpbot", cred.EMail)

			content, err := os.ReadFile(changesFile)
			if err != nil {
				if !os.IsNotExist(err) {
					return nil, CommitResult{}, fmt.Errorf("failed to read changes file %s: %w", changesFile, err)
				}
				content = []byte{}
			}

			newContent := append([]byte(changesEntry), content...)
			err = os.WriteFile(changesFile, newContent, 0644)
			if err != nil {
				return nil, CommitResult{}, fmt.Errorf("failed to write to changes file %s: %w", changesFile, err)
			}
		}
	}
	// get the remote files so that we know what to commit
	remoteFiles, err := cred.getRemoteFileList(ctx, projectName, bundleName)
	if err != nil {
		return nil, CommitResult{}, fmt.Errorf("failed to get remote file list: %w", err)
	}
	remoteFileMap := make(map[string]Entry)
	for _, entry := range remoteFiles.Entries {
		remoteFileMap[entry.Name] = entry
	}

	localFiles, err := os.ReadDir(params.Directory)
	if err != nil {
		return nil, CommitResult{}, fmt.Errorf("failed to read local directory: %w", err)
	}

	var changedFiles []string
	var newFiles []string
	localFileMap := make(map[string]bool)

	for _, file := range localFiles {
		if file.IsDir() {
			continue
		}
		fileName := file.Name()
		if strings.HasPrefix(fileName, ".") {
			continue // Ignore hidden files like .osc
		}
		localFileMap[fileName] = true
		filePath := filepath.Join(params.Directory, fileName)

		hash, err := fileMD5(filePath)
		if err != nil {
			return nil, CommitResult{}, fmt.Errorf("failed to calculate md5 for %s: %w", fileName, err)
		}

		remoteEntry, exists := remoteFileMap[fileName]
		if !exists {
			newFiles = append(newFiles, fileName)
		} else if remoteEntry.Md5 != hash {
			changedFiles = append(changedFiles, fileName)
		}
	}
	filesToUpload := append(newFiles, changedFiles...)
	if len(filesToUpload) > 0 {
		slog.Info("Uploading changed files", "files", filesToUpload)
		for _, fileName := range filesToUpload {
			filePath := filepath.Join(params.Directory, fileName)
			err := cred.uploadFile(ctx, projectName, bundleName, fileName, filePath)
			if err != nil {
				return nil, CommitResult{}, fmt.Errorf("failed to upload file %s: %w", fileName, err)
			}
		}
	} else {
		slog.Info("No changed files to upload")
	}

	allLocalFiles, err := os.ReadDir(params.Directory)
	if err != nil {
		return nil, CommitResult{}, fmt.Errorf("failed to re-read local directory: %w", err)
	}

	commitDir := Directory{
		Name:    bundleName,
		Project: projectName,
	}
	for _, file := range allLocalFiles {
		if file.IsDir() {
			continue
		}
		fileName := file.Name()
		if strings.HasPrefix(fileName, ".") {
			continue
		}
		filePath := filepath.Join(params.Directory, fileName)
		info, err := file.Info()
		if err != nil {
			return nil, CommitResult{}, fmt.Errorf("failed to get file info for %s: %w", fileName, err)
		}
		hash, err := fileMD5(filePath)
		if err != nil {
			return nil, CommitResult{}, fmt.Errorf("failed to calculate md5 for %s: %w", fileName, err)
		}
		commitDir.Entries = append(commitDir.Entries, Entry{
			Name:  fileName,
			Md5:   hash,
			Size:  fmt.Sprintf("%d", info.Size()),
			Mtime: fmt.Sprintf("%d", info.ModTime().Unix()),
		})
	}

	xmlData, err := xml.MarshalIndent(commitDir, "", "  ")
	if err != nil {
		return nil, CommitResult{}, fmt.Errorf("failed to marshal commit xml: %w", err)
	}

	revision, err := cred.commitFiles(ctx, projectName, bundleName, params.Message, xmlData)
	if err != nil {
		return nil, CommitResult{}, fmt.Errorf("failed to commit changes: %w", err)
	}

	return nil, CommitResult{Revision: revision.Rev}, nil
}

func (cred *OSCCredentials) getRemoteFileList(ctx context.Context, project, pkg string) (*Directory, error) {
	url := fmt.Sprintf("%s/source/%s/%s", cred.GetAPiAddr(), project, pkg)
	req, err := cred.buildRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &Directory{}, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get remote file list: status %s, body: %s", resp.Status, string(body))
	}

	var dir Directory
	if err := xml.NewDecoder(resp.Body).Decode(&dir); err != nil {
		return nil, err
	}
	return &dir, nil
}

func fileMD5(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func (cred *OSCCredentials) uploadFile(ctx context.Context, project, pkg, fileName, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	url := fmt.Sprintf("%s/source/%s/%s/%s", cred.GetAPiAddr(), project, pkg, fileName)
	req, err := cred.buildRequest(ctx, "PUT", url, file)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to upload file: status %s, body: %s", resp.Status, string(body))
	}
	return nil
}

func (cred *OSCCredentials) commitFiles(ctx context.Context, project, pkg, message string, xmlData []byte) (*Revision, error) {
	escapedMessage := url.QueryEscape(message)
	url := fmt.Sprintf("%s/source/%s/%s?cmd=commit&comment=%s", cred.GetAPiAddr(), project, pkg, escapedMessage)
	slog.Debug("Committing to OBS", "url", url)
	req, err := cred.buildRequest(ctx, "POST", url, bytes.NewReader(xmlData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/xml")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to commit: status %s, body: %s", resp.Status, string(body))
	}
	var revision Revision
	if err := xml.NewDecoder(resp.Body).Decode(&revision); err != nil {
		return nil, err
	}
	return &revision, nil
}

func createChangesEntry(message, userName, userEmail string) string {
	var b strings.Builder
	b.WriteString("-------------------------------------------------------------------\n")
	b.WriteString(time.Now().UTC().Format("Mon Jan 02 15:04:05 MST 2006"))
	b.WriteString(fmt.Sprintf(" - %s <%s>\n\n", userName, userEmail))

	lines := strings.Split(message, "\n")
	for _, line := range lines {
		if trimmedLine := strings.TrimSpace(line); trimmedLine != "" {
			b.WriteString(fmt.Sprintf("- %s\n", trimmedLine))
		}
	}
	b.WriteString("\n")
	return b.String()
}
